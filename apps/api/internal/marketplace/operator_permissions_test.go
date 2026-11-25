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

func TestOperatorPermissionGrantLifecycleAndRevocationLockPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()

	schema := "operator_permissions_test_" + fmt.Sprint(time.Now().UnixNano())
	if _, err = adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = adminPool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
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

	for _, name := range []string{
		"001_walking_skeleton.sql", "002_identity.sql",
		"022_identity_memberships.sql", "031_operator_action_grants.sql",
		"032_operator_permission_event_integrity.sql",
	} {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = execMigrationScript(ctx, pool, raw); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	adminID := newTestIdentity(t, ctx, pool)
	targetID := newTestIdentity(t, ctx, pool)
	outsiderID := newTestIdentity(t, ctx, pool)
	tenantID := "operator-permissions-" + newTestID(t)
	for _, identityID := range []string{adminID, targetID} {
		if _, err = pool.Exec(ctx, `
			INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
			VALUES($1,$2,$3,'operator')
		`, "membership-"+identityID, identityID, tenantID); err != nil {
			t.Fatal(err)
		}
	}

	bootstrapGrantID := "bootstrap-manage-" + newTestID(t)
	if _, err = pool.Exec(ctx, `
		INSERT INTO operator_action_grants
			(id,identity_id,tenant_id,action,granted_by_kind,grant_reason)
		VALUES($1,$2,$3,'operator:permissions:manage','system_bootstrap','Initial permission administrator')
	`, bootstrapGrantID, adminID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO operator_permission_events
			(grant_id,tenant_id,identity_id,action,event_type,actor_kind,reason,after_state)
		VALUES($1,$2,$3,'operator:permissions:manage','granted','system_bootstrap',
			'Initial permission administrator','{"active":true}'::jsonb)
	`, bootstrapGrantID, tenantID, adminID); err != nil {
		t.Fatal(err)
	}

	store := &Store{DB: pool}
	administrator := Actor{SubjectID: adminID, TenantID: tenantID, Role: RoleOperator}
	target := Actor{SubjectID: targetID, TenantID: tenantID, Role: RoleOperator}
	outsider := Actor{SubjectID: outsiderID, TenantID: tenantID, Role: RoleOperator}

	checkAuthorization := func(actor Actor, resource Resource, action Action) error {
		checkTx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			return beginErr
		}
		checkErr := authorizeOperatorTx(ctx, checkTx, actor, resource, action)
		_ = checkTx.Rollback(ctx)
		return checkErr
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = authorizeOperatorTx(ctx, tx, administrator, Resource{TenantID: tenantID}, ActionOperatorPermissionsManage); err != nil {
		t.Fatalf("explicit management grant should authorize: %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = authorizeOperatorTx(ctx, tx, administrator, Resource{TenantID: tenantID}, ActionOrderRead); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing action grant should deny: %v", err)
	}
	_ = tx.Rollback(ctx)
	if err = checkAuthorization(administrator, Resource{TenantID: "other-tenant"}, ActionOperatorPermissionsManage); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-tenant authorization should deny: %v", err)
	}
	if err = checkAuthorization(outsider, Resource{TenantID: tenantID}, ActionOperatorPermissionsManage); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing operator membership and grant should deny: %v", err)
	}
	if err = checkAuthorization(administrator, Resource{TenantID: tenantID}, Action("root")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unknown action should deny: %v", err)
	}

	if _, err = store.GrantOperatorAction(ctx, administrator, tenantID, adminID, ActionOrderRead, "self grant"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self-grant should deny: %v", err)
	}
	if _, err = store.GrantOperatorAction(ctx, administrator, tenantID, outsiderID, ActionOrderRead, "target is not an operator"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("grant to non-operator membership should deny: %v", err)
	}
	if _, err = store.GrantOperatorAction(ctx, administrator, "other-tenant", targetID, ActionOrderRead, "cross tenant"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-tenant grant should deny: %v", err)
	}

	grant, err := store.GrantOperatorAction(ctx, administrator, tenantID, targetID, ActionOrderRead, "Read operational orders")
	if err != nil {
		t.Fatalf("grant target action: %v", err)
	}
	if grant.ID == "" || grant.IdentityID != targetID || grant.Action != ActionOrderRead {
		t.Fatalf("unexpected grant result: %+v", grant)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = authorizeOperatorTx(ctx, tx, target, Resource{TenantID: tenantID}, ActionOrderRead); err != nil {
		t.Fatalf("target's explicit grant should authorize: %v", err)
	}
	if err = authorizeOperatorTx(ctx, tx, target, Resource{TenantID: tenantID}, ActionOrderAssign); !errors.Is(err, ErrForbidden) {
		t.Fatalf("action isolation should deny ungranted action: %v", err)
	}

	// The protected operation's FOR SHARE lock must prevent a concurrent
	// revocation from taking FOR UPDATE until that transaction completes.
	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = authorizeOperatorTx(ctx, lockTx, target, Resource{TenantID: tenantID}, ActionOrderRead); err != nil {
		t.Fatal(err)
	}
	probeTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var lockedID string
	err = probeTx.QueryRow(ctx, `
		SELECT id FROM operator_action_grants WHERE id=$1 FOR UPDATE NOWAIT
	`, grant.ID).Scan(&lockedID)
	if err == nil {
		_ = probeTx.Rollback(ctx)
		_ = lockTx.Rollback(ctx)
		t.Fatal("revocation lock unexpectedly bypassed the in-flight protected operation")
	}
	_ = probeTx.Rollback(ctx)
	if err = lockTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = store.RevokeOperatorAction(ctx, administrator, tenantID, bootstrapGrantID, "self revoke management"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self-revocation of permission management should deny: %v", err)
	}
	revoked, err := store.RevokeOperatorAction(ctx, administrator, tenantID, grant.ID, "Access no longer required")
	if err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	if revoked.RevokedAt == nil || revoked.RevokedByID == nil || *revoked.RevokedByID != adminID {
		t.Fatalf("revocation metadata missing: %+v", revoked)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = authorizeOperatorTx(ctx, tx, target, Resource{TenantID: tenantID}, ActionOrderRead); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked grant should deny: %v", err)
	}
	_ = tx.Rollback(ctx)

	var events int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM operator_permission_events WHERE grant_id=$1", grant.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Fatalf("grant and revoke must each append one event; got %d", events)
	}
	if _, err = pool.Exec(ctx, "UPDATE operator_permission_events SET reason='tampered' WHERE grant_id=$1", grant.ID); err == nil {
		t.Fatal("permission event mutation unexpectedly succeeded")
	}
}
