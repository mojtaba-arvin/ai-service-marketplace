package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrIncidentReportNotFound = errors.New("incident report not found")
	ErrIncidentReportInput    = errors.New("invalid incident report input")
)

type IncidentReportInput struct {
	OrderID       string
	Category      string
	Narrative     string
	OccurredAt    *time.Time
	SeverityHint  string
	EvidenceRefs  []string
}

type IncidentReport struct {
	ID                string     `json:"id"`
	OrderID           string     `json:"order_id"`
	ReporterRole      Role       `json:"reporter_role"`
	Category          string     `json:"category"`
	Narrative         string     `json:"narrative"`
	OccurredAt        *time.Time `json:"occurred_at,omitempty"`
	SeverityHint      string     `json:"severity_hint"`
	Status            string     `json:"status"`
	EvidenceReferences []string  `json:"evidence_references"`
	CreatedAt         time.Time  `json:"created_at"`
}

func validIncidentCategory(value string) bool {
	if len(value) < 2 || len(value) > 50 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, ch := range value[1:] {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

func validOpaqueEvidenceReference(value string) bool {
	if len(value) < 8 || len(value) > 200 {
		return false
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

// CreateIncidentReport accepts reports only from a customer who owns the order
// or a professional assigned to it. Tenant scope comes from the locked order row;
// request data can never choose or override the tenant.
func (s *Store) CreateIncidentReport(ctx context.Context, actor Actor, input IncidentReportInput) (IncidentReport, error) {
	if (actor.Role != RoleCustomer && actor.Role != RoleProfessional) ||
		actor.SubjectID == "" || actor.TenantID == "" {
		return IncidentReport{}, ErrForbidden
	}
	input.OrderID = strings.TrimSpace(input.OrderID)
	input.Category = strings.TrimSpace(input.Category)
	input.Narrative = strings.TrimSpace(input.Narrative)
	if input.SeverityHint == "" {
		input.SeverityHint = "unknown"
	}
	if input.OrderID == "" || len(input.OrderID) > 200 ||
		!validIncidentCategory(input.Category) || len(input.Narrative) < 10 || len(input.Narrative) > 4000 ||
		(input.SeverityHint != "unknown" && input.SeverityHint != "low" &&
			input.SeverityHint != "high" && input.SeverityHint != "critical") ||
		len(input.EvidenceRefs) > 5 {
		return IncidentReport{}, ErrIncidentReportInput
	}
	if input.OccurredAt != nil {
		occurred := input.OccurredAt.UTC()
		if occurred.After(time.Now().UTC().Add(5 * time.Minute)) {
			return IncidentReport{}, ErrIncidentReportInput
		}
		input.OccurredAt = &occurred
	}
	refs := make([]string, 0, len(input.EvidenceRefs))
	seen := make(map[string]struct{}, len(input.EvidenceRefs))
	for _, ref := range input.EvidenceRefs {
		if !validOpaqueEvidenceReference(ref) {
			return IncidentReport{}, ErrIncidentReportInput
		}
		if _, exists := seen[ref]; exists {
			return IncidentReport{}, ErrIncidentReportInput
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	evidenceJSON, err := json.Marshal(refs)
	if err != nil {
		return IncidentReport{}, err
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return IncidentReport{}, err
	}
	defer tx.Rollback(ctx)

	var customerID string
	var orderTenantID *string
	var assignedIdentityID *string
	err = tx.QueryRow(ctx, `
		SELECT o.customer_id,o.tenant_id,p.identity_id
		FROM orders o
		LEFT JOIN provider_profiles p
		  ON p.id=o.assigned_provider_id AND p.tenant_id=o.tenant_id
		WHERE o.id=$1
		FOR SHARE OF o
	`, input.OrderID).Scan(&customerID, &orderTenantID, &assignedIdentityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return IncidentReport{}, ErrForbidden
	}
	if err != nil {
		return IncidentReport{}, err
	}
	if orderTenantID == nil || *orderTenantID != actor.TenantID {
		return IncidentReport{}, ErrForbidden
	}

	switch actor.Role {
	case RoleCustomer:
		if customerID != actor.SubjectID {
			return IncidentReport{}, ErrForbidden
		}
	case RoleProfessional:
		if assignedIdentityID == nil || *assignedIdentityID != actor.SubjectID {
			return IncidentReport{}, ErrForbidden
		}
	}

	var membershipID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM identity_memberships
		WHERE identity_id=$1 AND tenant_id=$2 AND role=$3
		FOR SHARE
	`, actor.SubjectID, *orderTenantID, string(actor.Role)).Scan(&membershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		return IncidentReport{}, ErrForbidden
	}
	if err != nil {
		return IncidentReport{}, err
	}

	id, err := newID()
	if err != nil {
		return IncidentReport{}, err
	}
	var result IncidentReport
	result.ID = id
	result.OrderID = input.OrderID
	result.ReporterRole = actor.Role
	result.Category = input.Category
	result.Narrative = input.Narrative
	result.OccurredAt = input.OccurredAt
	result.SeverityHint = input.SeverityHint
	result.Status = "submitted"
	result.EvidenceReferences = refs
	err = tx.QueryRow(ctx, `
		INSERT INTO incident_reports
			(id,order_id,tenant_id,reporter_identity_id,reporter_role,category,narrative,
			 occurred_at,severity_hint,evidence_refs)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)
		RETURNING created_at
	`, id, input.OrderID, *orderTenantID, actor.SubjectID, string(actor.Role),
		input.Category, input.Narrative, input.OccurredAt, input.SeverityHint, evidenceJSON,
	).Scan(&result.CreatedAt)
	if err != nil {
		return IncidentReport{}, err
	}
	after, err := json.Marshal(map[string]string{
		"status": result.Status, "severity_hint": result.SeverityHint, "category": result.Category,
	})
	if err != nil {
		return IncidentReport{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO incident_report_events
			(report_id,tenant_id,actor_identity_id,action,reason,before_state,after_state)
		VALUES($1,$2,$3,'submitted','report submitted by order participant','{}'::jsonb,$4::jsonb)
	`, id, *orderTenantID, actor.SubjectID, after); err != nil {
		return IncidentReport{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return IncidentReport{}, err
	}
	return result, nil
}

// GetIncidentReportForReporter deliberately exposes no reviewer or support-role
// shortcut: only the report's original reporter can read this initial surface.
func (s *Store) GetIncidentReportForReporter(ctx context.Context, actor Actor, reportID string) (IncidentReport, error) {
	if (actor.Role != RoleCustomer && actor.Role != RoleProfessional) ||
		actor.SubjectID == "" || actor.TenantID == "" || strings.TrimSpace(reportID) == "" {
		return IncidentReport{}, ErrForbidden
	}
	var result IncidentReport
	var evidenceJSON []byte
	err := s.DB.QueryRow(ctx, `
		SELECT r.id,r.order_id,r.reporter_role,r.category,r.narrative,r.occurred_at,r.severity_hint,
			r.status,r.evidence_refs,r.created_at
		FROM incident_reports r
		JOIN identity_memberships m ON m.identity_id=r.reporter_identity_id
		 AND m.tenant_id=r.tenant_id AND m.role=r.reporter_role
		WHERE r.id=$1 AND r.tenant_id=$2 AND r.reporter_identity_id=$3 AND r.reporter_role=$4
	`, reportID, actor.TenantID, actor.SubjectID, string(actor.Role)).Scan(
		&result.ID, &result.OrderID, &result.ReporterRole, &result.Category, &result.Narrative,
		&result.OccurredAt, &result.SeverityHint, &result.Status, &evidenceJSON, &result.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return IncidentReport{}, ErrIncidentReportNotFound
	}
	if err != nil {
		return IncidentReport{}, err
	}
	if err = json.Unmarshal(evidenceJSON, &result.EvidenceReferences); err != nil {
		return IncidentReport{}, err
	}
	return result, nil
}
