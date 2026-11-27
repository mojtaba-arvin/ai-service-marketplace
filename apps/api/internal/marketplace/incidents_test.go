package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIncidentInputValidation(t *testing.T) {
	if !validIncidentCategory("safety_concern") || validIncidentCategory("Safety") ||
		validIncidentCategory("x") || validIncidentCategory("unsafe/category") {
		t.Fatal("incident category validation is not fail-closed")
	}
	if !validOpaqueEvidenceReference("private-object-001") ||
		validOpaqueEvidenceReference("https://example.com/object") ||
		validOpaqueEvidenceReference("../private-object") ||
		validOpaqueEvidenceReference("short") {
		t.Fatal("evidence reference validation is not restricted to opaque identifiers")
	}
}

func TestIncidentReportOrderParticipantPrivacyPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	schema := "incident_test_" + fmt.Sprint(time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	}()

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	migrations := []string{
		"001_walking_skeleton.sql", "002_identity.sql", "004_catalog.sql",
		"005_pricing.sql", "006_provider_eligibility.sql", "015_order_schedule.sql",
		"018_dispatch_offers.sql", "022_identity_memberships.sql",
		"023_order_tenant_scope.sql", "031_operator_action_grants.sql",
		"032_operator_permission_event_integrity.sql", "033_incident_report_intake.sql", "034_incident_reviewer_workflow.sql", "035_private_incident_evidence.sql",
	}
	for _, name := range migrations {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = execMigrationScript(ctx, pool, raw); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	customerID := newTestIdentity(t, ctx, pool)
	assignedProfessionalID := newTestIdentity(t, ctx, pool)
	unassignedProfessionalID := newTestIdentity(t, ctx, pool)
	outsiderID := newTestIdentity(t, ctx, pool)
	tenantID := "incident-tenant-" + newTestID(t)

	for _, membership := range []struct {
		identity string
		role     Role
	}{
		{customerID, RoleCustomer},
		{assignedProfessionalID, RoleProfessional},
		{unassignedProfessionalID, RoleProfessional},
		{outsiderID, RoleCustomer},
	} {
		if _, err = pool.Exec(ctx, `
			INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
			VALUES($1,$2,$3,$4)
		`, "membership-"+membership.identity, membership.identity, tenantID, string(membership.role)); err != nil {
			t.Fatal(err)
		}
	}
	assignedProviderID := "provider-" + newTestID(t)
	unassignedProviderID := "provider-" + newTestID(t)
	for _, provider := range []struct {
		id       string
		identity string
		name     string
	}{
		{assignedProviderID, assignedProfessionalID, "Assigned professional"},
		{unassignedProviderID, unassignedProfessionalID, "Unassigned professional"},
	} {
		if _, err = pool.Exec(ctx, `
			INSERT INTO provider_profiles(id,identity_id,tenant_id,display_name)
			VALUES($1,$2,$3,$4)
		`, provider.id, provider.identity, tenantID, provider.name); err != nil {
			t.Fatal(err)
		}
	}
	orderID := "incident-order-" + newTestID(t)
	if _, err = pool.Exec(ctx, `
		INSERT INTO orders(id,customer_id,idempotency_key,amount_minor,currency,status,tenant_id,assigned_provider_id)
		VALUES($1,$2,$3,1000,'OMR','requested',$4,$5)
	`, orderID, customerID, "incident-key-"+newTestID(t), tenantID, assignedProviderID); err != nil {
		t.Fatal(err)
	}

	store := &Store{DB: pool}
	reviewerID := newTestIdentity(t, ctx, pool)
	targetReviewerID := newTestIdentity(t, ctx, pool)
	unprivilegedReviewerID := newTestIdentity(t, ctx, pool)
	supportID := newTestIdentity(t, ctx, pool)
	managerID := newTestIdentity(t, ctx, pool)
	otherTenant := "incident-other-tenant-" + time.Now().Format("150405.000000000")
	reviewer := Actor{SubjectID: reviewerID, TenantID: tenantID, Role: RoleOperator}
	targetReviewer := Actor{SubjectID: targetReviewerID, TenantID: tenantID, Role: RoleOperator}
	manager := Actor{SubjectID: managerID, TenantID: tenantID, Role: RoleOperator}
	otherTenantReviewer := Actor{SubjectID: newTestIdentity(t, ctx, pool), TenantID: otherTenant, Role: RoleOperator}
	if err = ensureTestOperatorGrants(ctx, pool, reviewer, ActionIncidentReview); err != nil { t.Fatal(err) }
	if err = ensureTestOperatorGrants(ctx, pool, targetReviewer, ActionIncidentReview); err != nil { t.Fatal(err) }
	if err = ensureTestOperatorGrants(ctx, pool, manager, ActionOperatorPermissionsManage); err != nil { t.Fatal(err) }
	if err = ensureTestOperatorGrants(ctx, pool, otherTenantReviewer, ActionIncidentReview); err != nil { t.Fatal(err) }
	if _, err = pool.Exec(ctx, `
		INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
		VALUES($1,$2,$3,'operator'),($4,$5,$3,'support')
	`, "unprivileged-reviewer-"+time.Now().Format("150405.000000000"), unprivilegedReviewerID, tenantID,
		"support-reviewer-"+time.Now().Format("150405.000000000"), supportID); err != nil { t.Fatal(err) }
	unprivilegedReviewer := Actor{SubjectID: unprivilegedReviewerID, TenantID: tenantID, Role: RoleOperator}
	supportReviewer := Actor{SubjectID: supportID, TenantID: tenantID, Role: RoleSupport}
	customer := Actor{SubjectID: customerID, TenantID: tenantID, Role: RoleCustomer}
	report, err := store.CreateIncidentReport(ctx, customer, IncidentReportInput{
		OrderID: orderID, Category: "safety_concern",
		Narrative: "The service area was left in an unsafe condition.",
		EvidenceRefs: []string{"private-object-opaque-001"},
	})
	if err != nil {
		t.Fatalf("customer intake: %v", err)
	}
	if report.Status != "submitted" || report.SeverityHint != "unknown" {
		t.Fatalf("unexpected report defaults: %+v", report)
	}
	own, err := store.GetIncidentReportForReporter(ctx, customer, report.ID)
	if err != nil || own.ID != report.ID {
		t.Fatalf("reporter read: %+v %v", own, err)
	}
	if _, err = store.GetIncidentReportForReporter(ctx, Actor{
		SubjectID: outsiderID, TenantID: tenantID, Role: RoleCustomer,
	}, report.ID); !errors.Is(err, ErrIncidentReportNotFound) {
		t.Fatalf("unrelated customer read should be hidden: %v", err)
	}
	if _, err = store.GetIncidentReportForReporter(ctx, Actor{
		SubjectID: customerID, TenantID: "other-tenant", Role: RoleCustomer,
	}, report.ID); !errors.Is(err, ErrIncidentReportNotFound) {
		t.Fatalf("cross-tenant read should be hidden: %v", err)
	}

	assignedProfessional := Actor{SubjectID: assignedProfessionalID, TenantID: tenantID, Role: RoleProfessional}
	professionalReport, err := store.CreateIncidentReport(ctx, assignedProfessional, IncidentReportInput{
		OrderID: orderID, Category: "property_damage",
		Narrative: "A piece of equipment was damaged during the visit.",
		SeverityHint: "low",
	})
	if err != nil {
		t.Fatalf("assigned professional intake: %v", err)
	}
	if _, err = store.GetIncidentReportForReporter(ctx, assignedProfessional, professionalReport.ID); err != nil {
		t.Fatalf("professional reporter read: %v", err)
	}

	unassignedProfessional := Actor{SubjectID: unassignedProfessionalID, TenantID: tenantID, Role: RoleProfessional}
	if _, err = store.CreateIncidentReport(ctx, unassignedProfessional, IncidentReportInput{
		OrderID: orderID, Category: "safety_concern",
		Narrative: "An unrelated professional must not report against this order.",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unassigned professional intake should be denied: %v", err)
	}
	if _, err = store.CreateIncidentReport(ctx, Actor{
		SubjectID: customerID, TenantID: "other-tenant", Role: RoleCustomer,
	}, IncidentReportInput{
		OrderID: orderID, Category: "safety_concern",
		Narrative: "A tenant mismatch must not create an incident record.",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-tenant intake should be denied: %v", err)
	}
	if _, err = store.CreateIncidentReport(ctx, customer, IncidentReportInput{
		OrderID: orderID, Category: "safety_concern",
		Narrative: "Evidence reference must remain an opaque private identifier.",
		EvidenceRefs: []string{"https://example.com/evidence"},
	}); !errors.Is(err, ErrIncidentReportInput) {
		t.Fatalf("public evidence URL should be rejected: %v", err)
	}

	var events int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM incident_report_events WHERE report_id=$1", report.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("expected one atomic submission audit event, got %d", events)
	}

	if _, err = store.ListIncidentReportsForReviewer(ctx, unprivilegedReviewer, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("operator without incident:review must be denied: %v", err)
	}
	if _, err = store.ListIncidentReportsForReviewer(ctx, supportReviewer, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("support role alone must not authorize incident review: %v", err)
	}
	if _, err = store.ListIncidentReportsForReviewer(ctx, reviewer, "submitted"); err != nil {
		t.Fatalf("explicit reviewer list: %v", err)
	}
	if _, err = store.GetIncidentReportForReviewer(ctx, reviewer, "missing-report"); !errors.Is(err, ErrIncidentReportNotFound) {
		t.Fatalf("reviewer lookup should remain tenant-scoped: %v", err)
	}
	if _, err = store.GetIncidentReportForReviewer(ctx, otherTenantReviewer, report.ID); !errors.Is(err, ErrIncidentReportNotFound) {
		t.Fatalf("cross-tenant reviewer read should be hidden: %v", err)
	}
	if _, err = store.GetIncidentReportForReporter(ctx, reviewer, report.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reviewer must not use reporter read path: %v", err)
	}
	if _, err = store.TransitionIncidentReport(ctx, reviewer, report.ID, IncidentReportTransitionInput{
		Action:"assign",AssignedReviewerIdentityID:otherTenantReviewer.SubjectID,Reason:"cross-tenant assignment must fail",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-tenant reviewer assignment should be denied: %v", err)
	}
	assigned, err := store.TransitionIncidentReport(ctx, reviewer, report.ID, IncidentReportTransitionInput{
		Action:"assign",AssignedReviewerIdentityID:targetReviewerID,Reason:"assign to active same-tenant reviewer",
	})
	if err != nil || assigned.AssignedReviewerIdentityID == nil || *assigned.AssignedReviewerIdentityID != targetReviewerID {
		t.Fatalf("same-tenant reviewer assignment failed: %+v %v", assigned, err)
	}
	var targetGrantID string
	if err = pool.QueryRow(ctx, `
		SELECT id FROM operator_action_grants
		WHERE identity_id=$1 AND tenant_id=$2 AND action=$3 AND revoked_at IS NULL
	`, targetReviewerID, tenantID, string(ActionIncidentReview)).Scan(&targetGrantID); err != nil { t.Fatal(err) }
	if _, err = store.RevokeOperatorAction(ctx, manager, tenantID, targetGrantID, "reviewer access removed"); err != nil {
		t.Fatalf("revoke reviewer grant: %v", err)
	}
	if _, err = store.GetIncidentReportForReviewer(ctx, targetReviewer, report.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked reviewer must lose report access: %v", err)
	}
	assigned, err = store.TransitionIncidentReport(ctx, reviewer, report.ID, IncidentReportTransitionInput{
		Action:"assign",AssignedReviewerIdentityID:reviewerID,Reason:"reviewer takes ownership",
	})
	if err != nil || assigned.AssignedReviewerIdentityID == nil || *assigned.AssignedReviewerIdentityID != reviewerID {
		t.Fatalf("self-assignment by active reviewer failed: %+v %v", assigned, err)
	}
	if _, err = store.TransitionIncidentReport(ctx, reviewer, report.ID, IncidentReportTransitionInput{
		Action:"assign",AssignedReviewerIdentityID:targetReviewerID,Reason:"revoked reviewer cannot be assigned",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked reviewer assignment should be denied: %v", err)
	}
	if _, err = store.TransitionIncidentReport(ctx, reviewer, report.ID, IncidentReportTransitionInput{
		Action:"status",Status:"investigating",Reason:"skipping triage is prohibited",
	}); !errors.Is(err, ErrIncidentReportInput) {
		t.Fatalf("skipped status transition should be denied: %v", err)
	}
	updated, err := store.TransitionIncidentReport(ctx, reviewer, report.ID, IncidentReportTransitionInput{
		Action:"severity",SeverityHint:"high",Reason:"reviewer triage changed severity hint",
	})
	if err != nil || updated.SeverityHint != "high" {
		t.Fatalf("severity update failed: %+v %v", updated, err)
	}
	for _, next := range []string{"triaged","investigating","resolved","closed"} {
		updated, err = store.TransitionIncidentReport(ctx, reviewer, report.ID, IncidentReportTransitionInput{
			Action:"status",Status:next,Reason:"reviewer advanced the human workflow",
		})
		if err != nil || updated.Status != next {
			t.Fatalf("status transition to %s failed: %+v %v", next, updated, err)
		}
	}
	if _, err = store.TransitionIncidentReport(ctx, reviewer, report.ID, IncidentReportTransitionInput{
		Action:"severity",SeverityHint:"critical",Reason:"closed reports are immutable",
	}); !errors.Is(err, ErrIncidentReportInput) {
		t.Fatalf("closed report mutation should be denied: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE incident_reports SET narrative='rewritten by direct SQL' WHERE id=$1", report.ID); err == nil {
		t.Fatal("review workflow unexpectedly allowed reporter narrative mutation")
	}
	if _, err = pool.Exec(ctx, "UPDATE incident_reports SET status='closed' WHERE id=$1", professionalReport.ID); err == nil {
		t.Fatal("incident state changed without reviewer workflow and audit")
	}
	if _, err = pool.Exec(ctx, "UPDATE incident_report_events SET reason='tampered' WHERE report_id=$1", report.ID); err == nil {
		t.Fatal("incident audit event mutation unexpectedly succeeded")
	}
	if _, err = pool.Exec(ctx, "UPDATE incident_reports SET status='closed' WHERE id=$1", report.ID); err == nil {
		t.Fatal("incident state changed without reviewer workflow and audit")
	}
	if _, err = pool.Exec(ctx, "DELETE FROM incident_reports WHERE id=$1", report.ID); err == nil {
		t.Fatal("incident report deletion unexpectedly succeeded")
	}
}
