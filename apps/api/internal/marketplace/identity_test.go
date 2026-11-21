package marketplace

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGenerateOTPCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := GenerateOTPCode()
		if err != nil { t.Fatal(err) }
		if len(code) != otpCodeLength { t.Fatalf("code %q has length %d", code, len(code)) }
		for _, digit := range code { if digit < '0' || digit > '9' { t.Fatalf("code %q contains a non-digit", code) } }
		seen[code] = true
	}
	if len(seen) < 90 { t.Fatalf("unexpectedly low OTP diversity: %d", len(seen)) }
}

func TestOTPChallengeLifecyclePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "002_identity.sql"))
	if err != nil { t.Fatal(err) }
	if _, err = execMigrationScript(ctx, pool, migration); err != nil { t.Fatalf("identity migration: %v", err) }
	store := &Store{DB: pool}
	pepper := []byte("otp-pepper-material-must-be-at-least-32-bytes")
	phone := fmt.Sprintf("+1999%011d", time.Now().UnixNano()%100_000_000_000)
	code := "012345"
	id, err := store.CreateOTPChallenge(ctx, phone, code, pepper, 5*time.Minute, 3)
	if err != nil { t.Fatalf("create challenge: %v", err) }
	var stored string
	if err = pool.QueryRow(ctx, `SELECT encode(code_hash, 'hex') FROM otp_challenges WHERE id=$1`, id).Scan(&stored); err != nil { t.Fatal(err) }
	if stored == "" || stored != hex.EncodeToString(otpDigest(id, code, pepper)) { t.Fatal("OTP must be stored only as a keyed digest") }
	if _, err = store.VerifyOTPChallenge(ctx, id, "999999", pepper); !errors.Is(err, ErrOTPInvalidCode) { t.Fatalf("first invalid attempt: got %v", err) }
	if _, err = store.VerifyOTPChallenge(ctx, id, code, pepper); err != nil { t.Fatalf("valid code after one failed attempt: %v", err) }
	if _, err = store.VerifyOTPChallenge(ctx, id, code, pepper); !errors.Is(err, ErrOTPInvalidChallenge) { t.Fatalf("replay must fail: got %v", err) }

	exhausted, err := store.CreateOTPChallenge(ctx, phone, code, pepper, 5*time.Minute, 2)
	if err != nil { t.Fatalf("create exhausted challenge: %v", err) }
	if _, err = store.VerifyOTPChallenge(ctx, exhausted, "999999", pepper); !errors.Is(err, ErrOTPInvalidCode) { t.Fatalf("first guess: %v", err) }
	if _, err = store.VerifyOTPChallenge(ctx, exhausted, "888888", pepper); !errors.Is(err, ErrOTPInvalidCode) { t.Fatalf("last allowed guess: %v", err) }
	if _, err = store.VerifyOTPChallenge(ctx, exhausted, code, pepper); !errors.Is(err, ErrOTPInvalidChallenge) { t.Fatalf("exhausted challenge must fail: %v", err) }

	expired, err := store.CreateOTPChallenge(ctx, phone, code, pepper, time.Minute, 3)
	if err != nil { t.Fatalf("create expiring challenge: %v", err) }
	if _, err = pool.Exec(ctx, `UPDATE otp_challenges SET created_at=now()-interval '2 minutes', expires_at=now()-interval '1 second' WHERE id=$1`, expired); err != nil { t.Fatal(err) }
	if _, err = store.VerifyOTPChallenge(ctx, expired, code, pepper); !errors.Is(err, ErrOTPInvalidChallenge) { t.Fatalf("expired challenge must fail: %v", err) }

	replaced, err := store.CreateOTPChallenge(ctx, phone, code, pepper, 5*time.Minute, 3)
	if err != nil { t.Fatalf("create replaceable challenge: %v", err) }
	replacement, err := store.CreateOTPChallenge(ctx, phone, "543210", pepper, 5*time.Minute, 3)
	if err != nil { t.Fatalf("create replacement challenge: %v", err) }
	if _, err = store.VerifyOTPChallenge(ctx, replaced, code, pepper); !errors.Is(err, ErrOTPInvalidChallenge) { t.Fatalf("superseded challenge must fail: %v", err) }
	if _, err = store.VerifyOTPChallenge(ctx, replacement, "543210", pepper); err != nil { t.Fatalf("replacement challenge should verify: %v", err) }
	// Five challenges were created above; the next issuance must be rejected.
	if _, err = store.CreateOTPChallenge(ctx, phone, code, pepper, 5*time.Minute, 3); !errors.Is(err, ErrOTPRateLimited) { t.Fatalf("sixth issuance in one hour must be rate limited: %v", err) }
}
