package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSessionLifecyclePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	for _, name := range []string{"002_identity.sql", "003_sessions.sql"} {
		migration, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil { t.Fatal(readErr) }
		if _, err = execMigrationScript(ctx, pool, migration); err != nil { t.Fatalf("apply %s: %v", name, err) }
	}
	store := &Store{DB: pool}
	phone := fmt.Sprintf("+1998%011d", time.Now().UnixNano()%100_000_000_000)
	pepper := []byte("session-pepper-material-must-be-at-least-32-bytes")
	challenge, err := store.CreateOTPChallenge(ctx, phone, "112233", pepper, 5*time.Minute, 3)
	if err != nil { t.Fatal(err) }
	verifiedIdentity, err := store.VerifyOTPChallenge(ctx, challenge, "112233", pepper)
	if err != nil || verifiedIdentity.Phone() != phone { t.Fatalf("verify identity: phone=%s err=%v", verifiedIdentity.Phone(), err) }

	first, err := store.IssueSession(ctx, verifiedIdentity, pepper, 15*time.Minute, 24*time.Hour)
	if err != nil { t.Fatalf("issue session: %v", err) }
	if first.AccessToken == first.RefreshToken || len(first.AccessToken) < 40 || len(first.RefreshToken) < 40 {
		t.Fatal("session tokens must be distinct high-entropy opaque values")
	}
	var rawMatches int
	if err = pool.QueryRow(ctx, `
		SELECT count(*) FROM auth_sessions
		WHERE access_token_hash=$1 OR refresh_token_hash=$2
	`, []byte(first.AccessToken), []byte(first.RefreshToken)).Scan(&rawMatches); err != nil { t.Fatal(err) }
	if rawMatches != 0 { t.Fatal("raw bearer tokens must not be stored") }
	principal, err := store.AuthenticateAccessToken(ctx, first.AccessToken, pepper)
	if err != nil || principal.Phone != phone || principal.SessionID != first.SessionID {
		t.Fatalf("authenticate access token: %#v %v", principal, err)
	}
	second, err := store.RotateSession(ctx, first.RefreshToken, pepper, 15*time.Minute, 24*time.Hour)
	if err != nil { t.Fatalf("rotate refresh token: %v", err) }
	if _, err = store.AuthenticateAccessToken(ctx, first.AccessToken, pepper); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("old access token must be revoked after rotation: %v", err)
	}
	if _, err = store.AuthenticateAccessToken(ctx, second.AccessToken, pepper); err != nil { t.Fatalf("new access token should work: %v", err) }
	if _, err = store.RotateSession(ctx, first.RefreshToken, pepper, 15*time.Minute, 24*time.Hour); !errors.Is(err, ErrSessionReplay) {
		t.Fatalf("old refresh replay must be detected: %v", err)
	}
	if _, err = store.AuthenticateAccessToken(ctx, second.AccessToken, pepper); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("refresh replay must revoke the entire family: %v", err)
	}
	third, err := store.IssueSession(ctx, verifiedIdentity, pepper, 15*time.Minute, 24*time.Hour)
	if err != nil { t.Fatal(err) }
	if err = store.RevokeSession(ctx, third.AccessToken, pepper); err != nil { t.Fatal(err) }
	if _, err = store.AuthenticateAccessToken(ctx, third.AccessToken, pepper); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked access token must fail: %v", err)
	}


	// Concurrent refreshes of the same token must serialize. One rotation may
	// create a replacement, but the competing replay must revoke the whole family.
	concurrent, err := store.IssueSession(ctx, verifiedIdentity, pepper, 15*time.Minute, 24*time.Hour)
	if err != nil { t.Fatal(err) }
	type rotationResult struct {
		tokens SessionTokens
		err error
	}
	start := make(chan struct{})
	results := make(chan rotationResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tokens, rotateErr := store.RotateSession(ctx, concurrent.RefreshToken, pepper, 15*time.Minute, 24*time.Hour)
			results <- rotationResult{tokens: tokens, err: rotateErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, replays := 0, 0
	var rotated SessionTokens
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			rotated = result.tokens
		case errors.Is(result.err, ErrSessionReplay):
			replays++
		default:
			t.Fatalf("unexpected concurrent refresh result: %v", result.err)
		}
	}
	if successes != 1 || replays != 1 {
		t.Fatalf("concurrent refresh must yield one rotation and one replay: successes=%d replays=%d", successes, replays)
	}
	if _, err = store.AuthenticateAccessToken(ctx, rotated.AccessToken, pepper); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("competing refresh replay must revoke the replacement session family: %v", err)
	}
}
