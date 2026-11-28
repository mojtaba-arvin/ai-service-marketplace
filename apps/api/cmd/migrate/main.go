package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var migrationFiles = []string{
	"001_walking_skeleton.sql",
	"002_identity.sql",
	"003_sessions.sql",
	"004_catalog.sql",
	"005_pricing.sql",
	"006_provider_eligibility.sql",
	"007_market_config.sql",
	"008_catalog_localization.sql",
	"009_ledger_accounts.sql",
	"010_ledger_idempotency_reversal.sql",
	"011_payment_adapter_contract.sql",
	"012_settlement_reconciliation.sql",
	"013_inspection_quotes.sql",
	"014_quote_variations.sql",
	"015_order_schedule.sql",
	"016_cancellation_policy.sql",
	"017_provider_availability.sql",
	"018_dispatch_offers.sql",
	"019_outbox_delivery.sql",
	"020_professional_verification.sql",
	"021_support_cases.sql",
	"022_identity_memberships.sql",
	"023_order_tenant_scope.sql",
	"024_organizations_properties.sql",
	"025_auth_devices.sql",
	"026_market_config_audit.sql",
	"027_service_form_versions.sql",
	"028_order_service_form_answers.sql",
	"029_order_completion_approval.sql",
	"030_analytics_event_projection.sql",
	"031_operator_action_grants.sql",
	"032_operator_permission_event_integrity.sql",
	"033_incident_report_intake.sql",
	"034_incident_reviewer_workflow.sql",
	"035_private_incident_evidence.sql",
	"036_analytics_event_time_policy.sql",
}

const advisoryLockID int64 = 824713921734

func migrationChecksum(sql []byte) string {
	sum := sha256.Sum256(sql)
	return hex.EncodeToString(sum[:])
}

func run(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("configure database pool: %w", err)
	}
	defer pool.Close()

	return runWithPool(ctx, pool, migrationFiles, "migrations")
}

// runWithPool is shared by the production command and integration tests. Keeping
// the pool, ordered file list, and migration directory injectable lets CI prove
// legacy bootstrap, rollback/retry, and concurrent invocations against isolated
// PostgreSQL schemas without changing production configuration.
func runWithPool(ctx context.Context, pool *pgxpool.Pool, files []string, migrationDir string) error {
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockID); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, unlockErr := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", advisoryLockID); unlockErr != nil {
			log.Printf("WARNING: release migration advisory lock: %v", unlockErr)
		}
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			checksum text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, name := range files {
		sql, err := os.ReadFile(filepath.Join(migrationDir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		checksum := migrationChecksum(sql)

		var appliedChecksum string
		err = conn.QueryRow(ctx,
			"SELECT checksum FROM schema_migrations WHERE version = $1", name,
		).Scan(&appliedChecksum)
		if err == nil {
			if appliedChecksum != checksum {
				return fmt.Errorf(
					"migration %s checksum mismatch: database has %s, file has %s",
					name, appliedChecksum, checksum,
				)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check migration %s: %w", name, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err = tx.Exec(ctx, string(sql), pgx.QueryExecModeSimpleProtocol); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err = tx.Exec(ctx,
			"INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", name, checksum,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		log.Printf("applied migration %s", name)
	}

	log.Printf("database migrations complete (%d files)", len(files))
	return nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
