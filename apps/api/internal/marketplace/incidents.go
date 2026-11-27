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
	AssignedReviewerIdentityID *string `json:"assigned_reviewer_identity_id,omitempty"`
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


// IncidentReportTransitionInput describes one reviewer-owned change. Exactly
// one of Action=assign, severity, or status is accepted per transaction.
type IncidentReportTransitionInput struct {
	Action string
	Status string
	SeverityHint string
	AssignedReviewerIdentityID string
	Reason string
}

func validIncidentStatusTransition(from, to string) bool {
	switch from {
	case "submitted":
		return to == "triaged"
	case "triaged":
		return to == "investigating"
	case "investigating":
		return to == "resolved"
	case "resolved":
		return to == "closed"
	default:
		return false
	}
}

func validIncidentSeverity(value string) bool {
	return value == "unknown" || value == "low" || value == "high" || value == "critical"
}

func incidentReportState(status, severity string, reviewerID *string) map[string]any {
	return map[string]any{
		"status": status,
		"severity_hint": severity,
		"assigned_reviewer_identity_id": reviewerID,
	}
}

func scanIncidentReport(row pgx.Row) (IncidentReport, error) {
	var report IncidentReport
	var evidenceJSON []byte
	err := row.Scan(
		&report.ID, &report.OrderID, &report.ReporterRole, &report.Category,
		&report.Narrative, &report.OccurredAt, &report.SeverityHint, &report.Status,
		&evidenceJSON, &report.CreatedAt, &report.AssignedReviewerIdentityID,
	)
	if err != nil {
		return IncidentReport{}, err
	}
	if err = json.Unmarshal(evidenceJSON, &report.EvidenceReferences); err != nil {
		return IncidentReport{}, err
	}
	return report, nil
}

// ListIncidentReportsForReviewer returns only reports in the authenticated
// tenant and requires the dedicated incident:review grant, not role membership.
func (s *Store) ListIncidentReportsForReviewer(ctx context.Context, actor Actor, status string) ([]IncidentReport, error) {
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" {
		return nil, ErrForbidden
	}
	status = strings.TrimSpace(status)
	if status != "" && status != "submitted" && status != "triaged" &&
		status != "investigating" && status != "resolved" && status != "closed" {
		return nil, ErrIncidentReportInput
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return nil, err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: actor.TenantID}, ActionIncidentReview); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id,order_id,reporter_role,category,narrative,occurred_at,severity_hint,status,
			evidence_refs,created_at,assigned_reviewer_identity_id
		FROM incident_reports
		WHERE tenant_id=$1 AND ($2='' OR status=$2)
		ORDER BY created_at DESC,id DESC
		LIMIT 200
	`, actor.TenantID, status)
	if err != nil { return nil, err }
	reports := make([]IncidentReport, 0)
	for rows.Next() {
		report, scanErr := scanIncidentReport(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		reports = append(reports, report)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil { return nil, err }
	return reports, nil
}

// GetIncidentReportForReviewer is tenant-scoped and requires an active
// incident:review grant. Support role membership alone is never sufficient.
func (s *Store) GetIncidentReportForReviewer(ctx context.Context, actor Actor, reportID string) (IncidentReport, error) {
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" || strings.TrimSpace(reportID) == "" {
		return IncidentReport{}, ErrForbidden
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return IncidentReport{}, err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: actor.TenantID}, ActionIncidentReview); err != nil {
		return IncidentReport{}, err
	}
	report, err := scanIncidentReport(tx.QueryRow(ctx, `
		SELECT id,order_id,reporter_role,category,narrative,occurred_at,severity_hint,status,
			evidence_refs,created_at,assigned_reviewer_identity_id
		FROM incident_reports WHERE id=$1 AND tenant_id=$2
		FOR SHARE
	`, reportID, actor.TenantID))
	if errors.Is(err, pgx.ErrNoRows) { return IncidentReport{}, ErrIncidentReportNotFound }
	if err != nil { return IncidentReport{}, err }
	if err = tx.Commit(ctx); err != nil { return IncidentReport{}, err }
	return report, nil
}

// TransitionIncidentReport performs exactly one reasoned reviewer change. The
// matching audit event is inserted before the row update in the same transaction;
// migration 034's trigger rejects any update without that exact event.
func (s *Store) TransitionIncidentReport(ctx context.Context, actor Actor, reportID string, input IncidentReportTransitionInput) (IncidentReport, error) {
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" || strings.TrimSpace(reportID) == "" {
		return IncidentReport{}, ErrForbidden
	}
	input.Action = strings.TrimSpace(input.Action)
	input.Status = strings.TrimSpace(input.Status)
	input.SeverityHint = strings.TrimSpace(input.SeverityHint)
	input.AssignedReviewerIdentityID = strings.TrimSpace(input.AssignedReviewerIdentityID)
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Reason) < 1 || len(input.Reason) > 500 {
		return IncidentReport{}, ErrIncidentReportInput
	}
	switch input.Action {
	case "assign":
		if input.AssignedReviewerIdentityID == "" || len(input.AssignedReviewerIdentityID) > 200 ||
			input.Status != "" || input.SeverityHint != "" {
			return IncidentReport{}, ErrIncidentReportInput
		}
	case "severity":
		if !validIncidentSeverity(input.SeverityHint) || input.Status != "" || input.AssignedReviewerIdentityID != "" {
			return IncidentReport{}, ErrIncidentReportInput
		}
	case "status":
		if input.Status == "" || input.SeverityHint != "" || input.AssignedReviewerIdentityID != "" {
			return IncidentReport{}, ErrIncidentReportInput
		}
	default:
		return IncidentReport{}, ErrIncidentReportInput
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return IncidentReport{}, err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: actor.TenantID}, ActionIncidentReview); err != nil {
		return IncidentReport{}, err
	}
	current, err := scanIncidentReport(tx.QueryRow(ctx, `
		SELECT id,order_id,reporter_role,category,narrative,occurred_at,severity_hint,status,
			evidence_refs,created_at,assigned_reviewer_identity_id
		FROM incident_reports WHERE id=$1 AND tenant_id=$2
		FOR UPDATE
	`, reportID, actor.TenantID))
	if errors.Is(err, pgx.ErrNoRows) { return IncidentReport{}, ErrIncidentReportNotFound }
	if err != nil { return IncidentReport{}, err }
	if current.Status == "closed" { return IncidentReport{}, ErrIncidentReportInput }

	before := incidentReportState(current.Status, current.SeverityHint, current.AssignedReviewerIdentityID)
	afterStatus := current.Status
	afterSeverity := current.SeverityHint
	afterReviewer := current.AssignedReviewerIdentityID
	eventAction := ""
	switch input.Action {
	case "assign":
		if afterReviewer != nil && *afterReviewer == input.AssignedReviewerIdentityID {
			return IncidentReport{}, ErrIncidentReportInput
		}
		var membershipID, grantID string
		err = tx.QueryRow(ctx, `
			SELECT id FROM identity_memberships
			WHERE identity_id=$1 AND tenant_id=$2 AND role='operator'
			FOR SHARE
		`, input.AssignedReviewerIdentityID, actor.TenantID).Scan(&membershipID)
		if errors.Is(err, pgx.ErrNoRows) { return IncidentReport{}, ErrForbidden }
		if err != nil { return IncidentReport{}, err }
		err = tx.QueryRow(ctx, `
			SELECT id FROM operator_action_grants
			WHERE identity_id=$1 AND tenant_id=$2 AND action=$3 AND revoked_at IS NULL
			FOR SHARE
		`, input.AssignedReviewerIdentityID, actor.TenantID, string(ActionIncidentReview)).Scan(&grantID)
		if errors.Is(err, pgx.ErrNoRows) { return IncidentReport{}, ErrForbidden }
		if err != nil { return IncidentReport{}, err }
		afterReviewer = &input.AssignedReviewerIdentityID
		eventAction = "assigned"
	case "severity":
		if current.SeverityHint == input.SeverityHint { return IncidentReport{}, ErrIncidentReportInput }
		afterSeverity = input.SeverityHint
		eventAction = "severity_changed"
	case "status":
		if !validIncidentStatusTransition(current.Status, input.Status) { return IncidentReport{}, ErrIncidentReportInput }
		afterStatus = input.Status
		eventAction = "status_changed"
	}
	after := incidentReportState(afterStatus, afterSeverity, afterReviewer)
	beforeJSON, err := json.Marshal(before)
	if err != nil { return IncidentReport{}, err }
	afterJSON, err := json.Marshal(after)
	if err != nil { return IncidentReport{}, err }
	if _, err = tx.Exec(ctx, `
		INSERT INTO incident_report_events
			(report_id,tenant_id,actor_identity_id,action,reason,before_state,after_state)
		VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb)
	`, reportID, actor.TenantID, actor.SubjectID, eventAction, input.Reason, beforeJSON, afterJSON); err != nil {
		return IncidentReport{}, err
	}
	updated, err := scanIncidentReport(tx.QueryRow(ctx, `
		UPDATE incident_reports
		SET status=$3,severity_hint=$4,assigned_reviewer_identity_id=$5
		WHERE id=$1 AND tenant_id=$2
		RETURNING id,order_id,reporter_role,category,narrative,occurred_at,severity_hint,status,
			evidence_refs,created_at,assigned_reviewer_identity_id
	`, reportID, actor.TenantID, afterStatus, afterSeverity, afterReviewer))
	if err != nil { return IncidentReport{}, err }
	if err = tx.Commit(ctx); err != nil { return IncidentReport{}, err }
	return updated, nil
}
