package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestParseActions(t *testing.T) {
	actions, err := parseActions("analytics:read, incident:review, incident:evidence:manage, order:assign,order:read")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"analytics:read", "incident:evidence:manage", "incident:review", "order:assign", "order:read"}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("parseActions() = %#v, want %#v", actions, want)
	}
}

func TestParseActionsRejectsUnsafeInput(t *testing.T) {
	for _, raw := range []string{"", "order:read,", "order:read,order:read", "root", "ledger:post"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseActions(raw); err == nil {
				t.Fatalf("parseActions(%q) unexpectedly succeeded", raw)
			}
		})
	}
}

func TestBootstrapPostgresOneTimeAndImmutableAudit(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	schema := fmt.Sprintf("operator_bootstrap_test_%d", time.Now().UnixNano())
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

	baseMigration, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", "001_walking_skeleton.sql"))
	if readErr != nil { t.Fatal(readErr) }
	if _, err = pool.Exec(ctx, string(baseMigration), pgx.QueryExecModeSimpleProtocol); err != nil { t.Fatalf("apply 001_walking_skeleton.sql: %v", err) }
	if _, err = pool.Exec(ctx, "ALTER TABLE orders ADD COLUMN IF NOT EXISTS tenant_id text"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"002_identity.sql", "022_identity_memberships.sql", "031_operator_action_grants.sql", "032_operator_permission_event_integrity.sql", "033_incident_report_intake.sql", "034_incident_reviewer_workflow.sql", "035_private_incident_evidence.sql", "036_incident_evidence_lifecycle.sql"} {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = pool.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	identityID, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	tenantID := "bootstrap-test-tenant"
	phone := fmt.Sprintf("+1995%011d", time.Now().UnixNano()%100_000_000_000)
	if _, err = pool.Exec(ctx, "INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)", identityID, phone); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,$3,'operator')", "membership-"+identityID, identityID, tenantID); err != nil {
		t.Fatal(err)
	}

	actions, err := parseActions("incident:review,incident:evidence:manage,order:assign,order:read")
	if err != nil {
		t.Fatal(err)
	}
	reason := "Initial least-privilege deployment grant"
	if err = bootstrap(ctx, pool, tenantID, identityID, actions, reason); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	var grants, events, markers int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM operator_action_grants WHERE tenant_id=$1 AND identity_id=$2 AND revoked_at IS NULL", tenantID, identityID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM operator_permission_events WHERE tenant_id=$1 AND identity_id=$2 AND event_type='granted'", tenantID, identityID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM operator_permission_bootstraps WHERE tenant_id=$1", tenantID).Scan(&markers); err != nil {
		t.Fatal(err)
	}
	if grants != 4 || events != 4 || markers != 1 {
		t.Fatalf("unexpected bootstrap counts: grants=%d events=%d markers=%d", grants, events, markers)
	}

	if err = bootstrap(ctx, pool, tenantID, identityID, actions, reason); err == nil {
		t.Fatal("second tenant bootstrap unexpectedly succeeded")
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM operator_action_grants WHERE tenant_id=$1", tenantID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if grants != 4 {
		t.Fatalf("replayed bootstrap changed grants: %d", grants)
	}

	var grantID string
	if err = pool.QueryRow(ctx, "SELECT id FROM operator_action_grants WHERE tenant_id=$1 ORDER BY action LIMIT 1", tenantID).Scan(&grantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE operator_action_grants SET grant_reason='rewritten' WHERE id=$1", grantID); err == nil {
		t.Fatal("grant metadata mutation unexpectedly succeeded")
	}
	if _, err = pool.Exec(ctx, "DELETE FROM operator_action_grants WHERE id=$1", grantID); err == nil {
		t.Fatal("grant deletion unexpectedly succeeded")
	}
	if _, err = pool.Exec(ctx, "UPDATE operator_permission_events SET reason='rewritten' WHERE tenant_id=$1", tenantID); err == nil {
		t.Fatal("permission event mutation unexpectedly succeeded")
	}
	if _, err = pool.Exec(ctx, "DELETE FROM operator_permission_events WHERE tenant_id=$1", tenantID); err == nil {
		t.Fatal("permission event deletion unexpectedly succeeded")
	}

	// The audit event's tenant, identity and action must describe the referenced grant.
	var mismatchedAction string
	if err = pool.QueryRow(ctx, "SELECT CASE action WHEN 'order:assign' THEN 'order:read' ELSE 'order:assign' END FROM operator_action_grants WHERE id=$1", grantID).Scan(&mismatchedAction); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO operator_permission_events
			(grant_id,tenant_id,identity_id,action,event_type,actor_kind,reason,after_state)
		VALUES($1,$2,$3,$4,'granted','system_bootstrap','mismatched audit tuple','{}'::jsonb)
	`, grantID, tenantID, identityID, mismatchedAction); err == nil {
		t.Fatal("audit event with mismatched grant action unexpectedly succeeded")
	}

	if _, err = pool.Exec(ctx, "UPDATE operator_action_grants SET revoked_at=now(),revoked_by_identity_id=$2,revoke_reason='test revocation' WHERE id=$1", grantID, identityID); err != nil {
		t.Fatalf("one-way revocation should be permitted: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE operator_action_grants SET revoked_at=NULL,revoked_by_identity_id=NULL,revoke_reason='' WHERE id=$1", grantID); err == nil {
		t.Fatal("grant reactivation unexpectedly succeeded")
	}
	if _, err = pool.Exec(ctx, "DELETE FROM operator_permission_bootstraps WHERE tenant_id=$1", tenantID); err == nil {
		t.Fatal("bootstrap marker deletion unexpectedly succeeded")
	}

	if _, err = pool.Exec(ctx, "INSERT INTO operator_permission_bootstraps(tenant_id,bootstrap_identity_id,manifest_sha256,grants_count,created_by) VALUES($1,$2,$3,1,'system_bootstrap')", tenantID, identityID, fmt.Sprintf("%064x", 1)); err == nil {
		t.Fatal("duplicate bootstrap marker unexpectedly succeeded")
	}
}
