package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

var organizationSlugPattern = regexp.MustCompile(`^[a-z0-9]+([a-z0-9-]*[a-z0-9])?$`)
var countryCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)

var (
	ErrOrganizationWriteForbidden = errors.New("organization write forbidden")
	ErrOrganizationInput = errors.New("invalid organization or property input")
)

type CreateOrganizationInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type CreatePropertyInput struct {
	Code string `json:"property_code"`
	Name string `json:"name"`
	CountryCode string `json:"country_code"`
	RegionCode string `json:"region_code"`
	CityCode string `json:"city_code"`
	AreaCode string `json:"area_code"`
}

func validateCreateOrganization(in CreateOrganizationInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(in.Slug)
	if len(in.Name) < 1 || len(in.Name) > 160 || len(in.Slug) < 1 || len(in.Slug) > 160 || !organizationSlugPattern.MatchString(in.Slug) {
		return ErrOrganizationInput
	}
	return nil
}

func validateCreateProperty(in CreatePropertyInput) error {
	if len(strings.TrimSpace(in.Code)) < 1 || len(strings.TrimSpace(in.Code)) > 80 ||
		len(strings.TrimSpace(in.Name)) < 1 || len(strings.TrimSpace(in.Name)) > 160 ||
		!countryCodePattern.MatchString(strings.ToUpper(strings.TrimSpace(in.CountryCode))) ||
		len(strings.TrimSpace(in.RegionCode)) < 1 || len(strings.TrimSpace(in.RegionCode)) > 100 ||
		len(strings.TrimSpace(in.CityCode)) < 1 || len(strings.TrimSpace(in.CityCode)) > 100 ||
		len(strings.TrimSpace(in.AreaCode)) < 1 || len(strings.TrimSpace(in.AreaCode)) > 100 {
		return ErrOrganizationInput
	}
	return nil
}

// CreateOrganizationForIdentity creates the organization, its initial owner
// membership, immutable audit record, and canonical outbox event atomically.
func (s *Store) CreateOrganizationForIdentity(ctx context.Context, identityID, tenantID string, in CreateOrganizationInput) (OrganizationSummary, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(in.Slug)
	if strings.TrimSpace(identityID) == "" || strings.TrimSpace(tenantID) == "" || validateCreateOrganization(in) != nil {
		return OrganizationSummary{}, ErrOrganizationInput
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return OrganizationSummary{}, err }
	defer tx.Rollback(ctx)

	var identityRole string
	err = tx.QueryRow(ctx, `
		SELECT role FROM identity_memberships
		WHERE identity_id=$1 AND tenant_id=$2
		ORDER BY CASE role WHEN 'customer' THEN 0 WHEN 'professional' THEN 1 WHEN 'operator' THEN 2 ELSE 3 END
		LIMIT 1
		FOR SHARE
	`, identityID, tenantID).Scan(&identityRole)
	if errors.Is(err, pgx.ErrNoRows) { return OrganizationSummary{}, ErrOrganizationWriteForbidden }
	if err != nil { return OrganizationSummary{}, err }

	orgID, err := newID()
	if err != nil { return OrganizationSummary{}, err }
	_, err = tx.Exec(ctx, `
		INSERT INTO organizations(id,tenant_id,name,slug,created_by_identity_id,created_by_identity_role)
		VALUES($1,$2,$3,$4,$5,$6)
	`, orgID, tenantID, in.Name, in.Slug, identityID, identityRole)
	if err != nil { return OrganizationSummary{}, err }

	membershipID, err := newID()
	if err != nil { return OrganizationSummary{}, err }
	_, err = tx.Exec(ctx, `
		INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,status)
		VALUES($1,$2,$3,$4,$5,'owner','active')
	`, membershipID, orgID, tenantID, identityID, identityRole)
	if err != nil { return OrganizationSummary{}, err }

	if err = appendOrganizationAudit(ctx, tx, orgID, tenantID, identityID, identityRole, "organization.created", map[string]string{"organization_id": orgID}); err != nil {
		return OrganizationSummary{}, err
	}
	if err = appendOrganizationDomainEvent(ctx, tx, tenantID, "organization", orgID, "organization.created", map[string]string{"organization_id": orgID, "slug": in.Slug}); err != nil {
		return OrganizationSummary{}, err
	}
	if err = tx.Commit(ctx); err != nil { return OrganizationSummary{}, err }
	return OrganizationSummary{ID: orgID, TenantID: tenantID, Name: in.Name, Slug: in.Slug, Status: "active", Role: "owner"}, nil
}

// CreatePropertyForIdentity requires a currently active owner/admin/manager
// membership and commits the property, audit record, and canonical event together.
func (s *Store) CreatePropertyForIdentity(ctx context.Context, identityID, tenantID, organizationID string, in CreatePropertyInput) (PropertySummary, error) {
	in.Code = strings.TrimSpace(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	in.RegionCode = strings.TrimSpace(in.RegionCode)
	in.CityCode = strings.TrimSpace(in.CityCode)
	in.AreaCode = strings.TrimSpace(in.AreaCode)
	if strings.TrimSpace(identityID) == "" || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(organizationID) == "" || validateCreateProperty(in) != nil {
		return PropertySummary{}, ErrOrganizationInput
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return PropertySummary{}, err }
	defer tx.Rollback(ctx)

	var identityRole string
	err = tx.QueryRow(ctx, `
		SELECT om.identity_role
		FROM organization_memberships om
		JOIN organizations o ON o.id=om.organization_id AND o.tenant_id=om.tenant_id
		WHERE om.identity_id=$1 AND om.tenant_id=$2 AND om.organization_id=$3
		  AND om.status='active' AND o.status='active'
		  AND om.role IN ('owner','admin','manager')
		FOR SHARE OF om,o
	`, identityID, tenantID, organizationID).Scan(&identityRole)
	if errors.Is(err, pgx.ErrNoRows) { return PropertySummary{}, ErrOrganizationWriteForbidden }
	if err != nil { return PropertySummary{}, err }

	propertyID, err := newID()
	if err != nil { return PropertySummary{}, err }
	_, err = tx.Exec(ctx, `
		INSERT INTO properties(id,tenant_id,organization_id,property_code,name,country_code,region_code,city_code,area_code,created_by_identity_id,created_by_identity_role)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, propertyID, tenantID, organizationID, in.Code, in.Name, in.CountryCode, in.RegionCode, in.CityCode, in.AreaCode, identityID, identityRole)
	if err != nil { return PropertySummary{}, err }

	if err = appendOrganizationAudit(ctx, tx, organizationID, tenantID, identityID, identityRole, "property.created", map[string]string{"property_id": propertyID}); err != nil {
		return PropertySummary{}, err
	}
	if err = appendOrganizationDomainEvent(ctx, tx, tenantID, "property", propertyID, "property.created", map[string]string{"property_id": propertyID, "organization_id": organizationID, "property_code": in.Code}); err != nil {
		return PropertySummary{}, err
	}
	if err = tx.Commit(ctx); err != nil { return PropertySummary{}, err }
	return PropertySummary{ID: propertyID, OrganizationID: organizationID, TenantID: tenantID, Code: in.Code, Name: in.Name, CountryCode: in.CountryCode, RegionCode: in.RegionCode, CityCode: in.CityCode, AreaCode: in.AreaCode, Status: "active", AccessRole: "owner"}, nil
}

func appendOrganizationAudit(ctx context.Context, tx pgx.Tx, organizationID, tenantID, actorID, actorRole, action string, details map[string]string) error {
	id, err := newID()
	if err != nil { return err }
	payload, err := json.Marshal(details)
	if err != nil { return err }
	_, err = tx.Exec(ctx, `
		INSERT INTO organization_audit_events(id,organization_id,tenant_id,actor_identity_id,actor_identity_role,action,details)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)
	`, id, organizationID, tenantID, actorID, actorRole, action, payload)
	return err
}

func appendOrganizationDomainEvent(ctx context.Context, tx pgx.Tx, tenantID, aggregateType, aggregateID, eventType string, data map[string]string) error {
	id, err := newID()
	if err != nil { return err }
	payload, err := json.Marshal(data)
	if err != nil { return err }
	return appendDomainEvent(ctx, tx, outbox.DomainEvent{
		ID: id, Type: eventType, Version: 1, OccurredAt: time.Now().UTC(),
		TenantID: tenantID, Aggregate: outbox.EventAggregate{Type: aggregateType, ID: aggregateID}, Data: payload,
	})
}

func appendDomainEvent(ctx context.Context, tx pgx.Tx, event outbox.DomainEvent) error {
	_, err := outbox.AppendDomainEvent(ctx, tx, event)
	return err
}

func decodeOrganizationInput(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil { return err }
	var extra any
	if err := decoder.Decode(&extra); err == nil { return ErrOrganizationInput }
	return nil
}

func (h *Handler) organizationCreate(w http.ResponseWriter, r *http.Request) {
	principal, err := h.principal(r)
	if err != nil { writeError(w, http.StatusUnauthorized, "unauthorized"); return }
	var in CreateOrganizationInput
	if err = decodeOrganizationInput(r, &in); err != nil { writeError(w, http.StatusBadRequest, "invalid organization payload"); return }
	item, err := h.Store.CreateOrganizationForIdentity(r.Context(), principal.IdentityID, h.organizationTenant(r), in)
	if errors.Is(err, ErrOrganizationWriteForbidden) { writeError(w, http.StatusForbidden, "tenant membership is required"); return }
	if isOrganizationConflict(err) { writeError(w, http.StatusConflict, "organization slug already exists"); return }
	if errors.Is(err, ErrOrganizationInput) { writeError(w, http.StatusBadRequest, "invalid organization payload"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, "could not create organization"); return }
	writeJSON(w, http.StatusCreated, map[string]any{"organization": item})
}

func (h *Handler) organizationPropertyCreate(w http.ResponseWriter, r *http.Request, organizationID string) {
	principal, err := h.principal(r)
	if err != nil { writeError(w, http.StatusUnauthorized, "unauthorized"); return }
	var in CreatePropertyInput
	if err = decodeOrganizationInput(r, &in); err != nil { writeError(w, http.StatusBadRequest, "invalid property payload"); return }
	item, err := h.Store.CreatePropertyForIdentity(r.Context(), principal.IdentityID, h.organizationTenant(r), organizationID, in)
	if errors.Is(err, ErrOrganizationWriteForbidden) { writeError(w, http.StatusNotFound, "organization not found"); return }
	if isOrganizationConflict(err) { writeError(w, http.StatusConflict, "property code already exists"); return }
	if errors.Is(err, ErrOrganizationInput) { writeError(w, http.StatusBadRequest, "invalid property payload"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, "could not create property"); return }
	writeJSON(w, http.StatusCreated, map[string]any{"property": item})
}

func isOrganizationConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var _ = context.Background
