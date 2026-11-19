package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketplace"
)

func main() {
	if os.Getenv("APP_ENV") == "production" {
		log.Fatal("walking skeleton uses a test-only OTP/auth adapter and must not run in production")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" { log.Fatal("DATABASE_URL is required") }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { log.Fatal(err) }
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil { log.Fatal(err) }
	for _, name := range []string{"001_walking_skeleton.sql", "002_identity.sql", "003_sessions.sql", "004_catalog.sql", "005_pricing.sql", "006_provider_eligibility.sql", "007_market_config.sql", "008_catalog_localization.sql", "009_ledger_accounts.sql", "010_ledger_idempotency_reversal.sql"} {
		migration, readErr := os.ReadFile("migrations/" + name)
		if readErr != nil { log.Fatal(readErr) }
		if _, migrateErr := pool.Exec(ctx, string(migration)); migrateErr != nil { log.Fatalf("apply %s: %v", name, migrateErr) }
	}
	code := os.Getenv("TEST_OTP_CODE")
	if code == "" { code = "000000" }
	handler := &marketplace.Handler{Store: &marketplace.Store{DB: pool}, OTPCode: code}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" { addr = ":8080" }
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5*time.Second, ReadTimeout: 10*time.Second, WriteTimeout: 10*time.Second, IdleTimeout: 60*time.Second}
	log.Printf("test-only marketplace API listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
