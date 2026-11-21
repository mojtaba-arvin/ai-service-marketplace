package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func migrationTestPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create admin pool: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatalf("create isolated schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
		t.Fatalf("parse test database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
		t.Fatalf("create schema-scoped pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = admin.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	return pool, schema
}

func migrationTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func migrationHistoryCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migration history: %v", err)
	}
	return count
}

func TestRunAppliesMigrationsOnceAndSkipsCompletedVersions(t *testing.T) {
	pool, _ := migrationTestPool(t)
	ctx := migrationTestContext(t)
	dir := migrationDirectory(t)

	if err := runWithPool(ctx, pool, migrationFiles, dir); err != nil {
		t.Fatalf("initial migration run: %v", err)
	}
	if got := migrationHistoryCount(t, ctx, pool); got != len(migrationFiles) {
		t.Fatalf("recorded migration count = %d, want %d", got, len(migrationFiles))
	}

	if err := runWithPool(ctx, pool, migrationFiles, dir); err != nil {
		t.Fatalf("migration replay: %v", err)
	}
	if got := migrationHistoryCount(t, ctx, pool); got != len(migrationFiles) {
		t.Fatalf("replay changed migration history count to %d, want %d", got, len(migrationFiles))
	}
}

func TestRunBootstrapsLegacySchemaWithoutMigrationHistory(t *testing.T) {
	pool, _ := migrationTestPool(t)
	ctx := migrationTestContext(t)
	dir := migrationDirectory(t)

	// Simulate a database created by the previous API-startup migration path:
	// all schema migrations exist, but schema_migrations has never been created.
	for _, name := range migrationFiles {
		sql, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read legacy migration %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", name, err)
		}
	}

	if err := runWithPool(ctx, pool, migrationFiles, dir); err != nil {
		t.Fatalf("bootstrap legacy schema: %v", err)
	}
	if got := migrationHistoryCount(t, ctx, pool); got != len(migrationFiles) {
		t.Fatalf("legacy bootstrap recorded %d versions, want %d", got, len(migrationFiles))
	}
}

func TestRunSerializesConcurrentInvocations(t *testing.T) {
	pool, schema := migrationTestPool(t)
	ctx := migrationTestContext(t)
	dir := migrationDirectory(t)

	config, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	secondPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create second migration pool: %v", err)
	}
	t.Cleanup(secondPool.Close)

	results := make(chan error, 2)
	go func() { results <- runWithPool(ctx, pool, migrationFiles, dir) }()
	go func() { results <- runWithPool(ctx, secondPool, migrationFiles, dir) }()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent migration invocation %d: %v", i+1, err)
		}
	}
	if got := migrationHistoryCount(t, ctx, pool); got != len(migrationFiles) {
		t.Fatalf("concurrent runs recorded %d versions, want %d", got, len(migrationFiles))
	}
}

func TestRunRollsBackFailedMigrationAndCanRetry(t *testing.T) {
	pool, _ := migrationTestPool(t)
	ctx := migrationTestContext(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "001_retryable.sql")

	broken := "CREATE TABLE rollback_probe (id integer PRIMARY KEY); INSERT INTO missing_table(id) VALUES (1);"
	if err := os.WriteFile(file, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runWithPool(ctx, pool, []string{"001_retryable.sql"}, dir); err == nil {
		t.Fatal("expected the invalid migration to fail")
	}

	var tableExists, versionRecorded bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('rollback_probe') IS NOT NULL").Scan(&tableExists); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)", "001_retryable.sql").Scan(&versionRecorded); err != nil {
		t.Fatal(err)
	}
	if tableExists || versionRecorded {
		t.Fatalf("failed migration was not rolled back: table_exists=%v version_recorded=%v", tableExists, versionRecorded)
	}

	if err := os.WriteFile(file, []byte("CREATE TABLE rollback_probe (id integer PRIMARY KEY);"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runWithPool(ctx, pool, []string{"001_retryable.sql"}, dir); err != nil {
		t.Fatalf("retry after correcting failed migration: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT to_regclass('rollback_probe') IS NOT NULL").Scan(&tableExists); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)", "001_retryable.sql").Scan(&versionRecorded); err != nil {
		t.Fatal(err)
	}
	if !tableExists || !versionRecorded {
		t.Fatalf("successful retry was not committed: table_exists=%v version_recorded=%v", tableExists, versionRecorded)
	}
}
