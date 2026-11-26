package marketplace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/pgconn"
)

// execMigrationScript executes a complete migration file. Migration files are
// trusted repository SQL and intentionally contain multiple statements and
// dollar-quoted PL/pgSQL blocks, so they must use pgx's simple query protocol.
func execMigrationScript(ctx context.Context, pool *pgxpool.Pool, sql []byte) (pgconn.CommandTag, error) {
	return pool.Exec(ctx, string(sql), pgx.QueryExecModeSimpleProtocol)
}


// ensureTestOperatorGrants provisions an explicit operator membership and
// allowlisted grants for integration-test fixtures. It applies the grant and
// audit-integrity migrations idempotently so legacy fixture migration lists
// need not duplicate ADMIN-001 schema setup.
func ensureTestOperatorGrants(ctx context.Context, pool *pgxpool.Pool, actor Actor, actions ...Action) error {
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" || len(actions) == 0 {
		return ErrForbidden
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE orders ADD COLUMN IF NOT EXISTS tenant_id text"); err != nil {
		return fmt.Errorf("prepare order tenant key for incident schema: %w", err)
	}
	// Apply the migration chain once per isolated test schema. A schema-local
	// marker avoids replaying migration 034 after the evidence-manager grant is
	// seeded; 034 narrows the allowlist before migration 036 widens it again.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin operator grant fixture setup: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database() || ':' || current_schema() || ':test-operator-grants', 0))"); err != nil {
		return fmt.Errorf("lock operator grant fixture migrations: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _test_operator_grant_migration_state (
			migration_key text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create operator grant fixture migration marker: %w", err)
	}
	var migrationsApplied bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM _test_operator_grant_migration_state WHERE migration_key='trust-002-036')").Scan(&migrationsApplied); err != nil {
		return fmt.Errorf("read operator grant fixture migration marker: %w", err)
	}
	if !migrationsApplied {
		var evidenceManagerGrantExists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM operator_action_grants WHERE action='incident:evidence:manage')").Scan(&evidenceManagerGrantExists); err != nil {
			return fmt.Errorf("inspect evidence manager fixture grants: %w", err)
		}
		migrationNames := []string{
			"001_walking_skeleton.sql", "002_identity.sql", "022_identity_memberships.sql",
			"031_operator_action_grants.sql", "032_operator_permission_event_integrity.sql",
			"033_incident_report_intake.sql",
		}
		if !evidenceManagerGrantExists {
			migrationNames = append(migrationNames, "034_incident_reviewer_workflow.sql")
		}
		migrationNames = append(migrationNames, "035_private_incident_evidence.sql", "036_incident_evidence_lifecycle.sql")
		for _, name := range migrationNames {
			raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
			if readErr != nil {
				return fmt.Errorf("read %s: %w", name, readErr)
			}
			if _, err = tx.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
				return fmt.Errorf("apply %s: %w", name, err)
			}
		}
		if _, err = tx.Exec(ctx, "INSERT INTO _test_operator_grant_migration_state(migration_key) VALUES('trust-002-036')"); err != nil {
			return fmt.Errorf("record operator grant fixture migrations: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit operator grant fixture migrations: %w", err)
	}
	var identityExists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM phone_identities WHERE id=$1)", actor.SubjectID).Scan(&identityExists); err != nil {
		return fmt.Errorf("check operator identity: %w", err)
	}
	if !identityExists {
		phone := fmt.Sprintf("+1%014d", time.Now().UnixNano()%100000000000000)
		if _, err := pool.Exec(ctx, "INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)", actor.SubjectID, phone); err != nil {
			return fmt.Errorf("seed operator identity: %w", err)
		}
	}
	membershipID := "test-operator-membership-" + actor.SubjectID + "-" + actor.TenantID
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
		VALUES($1,$2,$3,'operator')
		ON CONFLICT(identity_id,tenant_id,role) DO NOTHING
	`, membershipID, actor.SubjectID, actor.TenantID); err != nil {
		return fmt.Errorf("seed operator membership: %w", err)
	}
	for _, action := range actions {
		if !validOperatorGrantAction(action) {
			return fmt.Errorf("invalid test operator action %q", action)
		}
		grantID, err := newID()
		if err != nil {
			return err
		}
		var insertedID string
		err = pool.QueryRow(ctx, `
			INSERT INTO operator_action_grants
				(id,identity_id,tenant_id,action,granted_by_kind,grant_reason)
			VALUES($1,$2,$3,$4,'system_bootstrap','Explicit integration-test fixture grant')
			ON CONFLICT DO NOTHING
			RETURNING id
		`, grantID, actor.SubjectID, actor.TenantID, string(action)).Scan(&insertedID)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("seed %s grant: %w", action, err)
		}
		if insertedID == "" {
			continue
		}
		if _, err = pool.Exec(ctx, `
			INSERT INTO operator_permission_events
				(grant_id,tenant_id,identity_id,action,event_type,actor_kind,reason,after_state)
			VALUES($1,$2,$3,$4,'granted','system_bootstrap',
				'Explicit integration-test fixture grant','{"active":true}'::jsonb)
		`, insertedID, actor.TenantID, actor.SubjectID, string(action)); err != nil {
			return fmt.Errorf("seed %s grant audit: %w", action, err)
		}
	}
	return nil
}
