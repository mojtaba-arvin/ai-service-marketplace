package marketplace

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/adapters"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketconfig"
)

func TestValidateExternalIdentityAssertion(t *testing.T) {
	now:=time.Now()
	valid:=adapters.VerifiedExternalIdentity{Provider:adapters.ProviderGoogle,Subject:"opaque-provider-subject",Nonce:"single-use-nonce",IssuedAt:now.Add(-time.Minute),ExpiresAt:now.Add(time.Minute)}
	if err:=validateExternalAssertion(valid,now);err!=nil{t.Fatalf("valid assertion rejected: %v",err)}
	cases:=[]adapters.VerifiedExternalIdentity{
		{Provider:"unknown",Subject:"subject",Nonce:"nonce",IssuedAt:now.Add(-time.Minute),ExpiresAt:now.Add(time.Minute)},
		{Provider:adapters.ProviderApple,Subject:"subject",Nonce:"nonce",IssuedAt:now.Add(-2*time.Minute),ExpiresAt:now.Add(-time.Minute)},
		{Provider:adapters.ProviderEmail,Subject:"subject",Nonce:"nonce",ExpiresAt:now.Add(time.Minute)},
		{Provider:adapters.ProviderGoogle,Subject:strings.Repeat("s",256),Nonce:"nonce",IssuedAt:now.Add(-time.Minute),ExpiresAt:now.Add(time.Minute)},
	}
	for _,assertion:=range cases{if !errors.Is(validateExternalAssertion(assertion,now),ErrExternalIdentityInvalid){t.Errorf("invalid assertion accepted: %#v",assertion)}}
}

func TestPostgresIdentityNonceStoreRejectsReplay(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
	conn,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer conn.Close(ctx)
	_,err=conn.Exec(ctx,`CREATE TEMP TABLE external_identity_link_nonces(
		identity_id text,provider text NOT NULL,nonce_hash bytea NOT NULL,expires_at timestamptz NOT NULL,consumed_at timestamptz
	)`)
	if err!=nil{t.Fatal(err)}
	nonce:="single-use-test-nonce"
	digest:=sha256.Sum256([]byte(nonce))
	expires:=time.Now().Add(time.Minute)
	if _,err=conn.Exec(ctx,`INSERT INTO external_identity_link_nonces(identity_id,provider,nonce_hash,expires_at) VALUES(NULL,'google',$1,$2)`,digest[:],expires);err!=nil{t.Fatal(err)}
	tx,err:=conn.Begin(ctx);if err!=nil{t.Fatal(err)}
	store:=PostgresIdentityNonceStore{}
	if err=store.Consume(ctx,tx,"","google",nonce,expires);err!=nil{t.Fatalf("first nonce consumption failed: %v",err)}
	if err=store.Consume(ctx,tx,"","google",nonce,expires);!errors.Is(err,ErrIdentityNonceReplay){t.Fatalf("replayed nonce was accepted: %v",err)}
	if err=tx.Commit(ctx);err!=nil{t.Fatal(err)}
}


type staticExternalIdentityVerifier struct {
	identity adapters.VerifiedExternalIdentity
}

func (v staticExternalIdentityVerifier) Verify(string) (adapters.VerifiedExternalIdentity, error) {
	return v.identity, nil
}

func TestResolveExternalIdentityConsumesLoginNonceOnlyAfterFindingLink(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1 // Keep temporary tables on the same PostgreSQL session.
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx, `
		CREATE TEMP TABLE phone_identities (
			id text PRIMARY KEY,
			phone_e164 text NOT NULL
		);
		CREATE TEMP TABLE external_identity_links (
			provider text NOT NULL,
			provider_subject text NOT NULL,
			phone_identity_id text NOT NULL REFERENCES phone_identities(id),
			PRIMARY KEY(provider, provider_subject)
		);
		CREATE TEMP TABLE external_identity_link_nonces (
			identity_id text,
			provider text NOT NULL,
			nonce_hash bytea NOT NULL,
			expires_at timestamptz NOT NULL,
			consumed_at timestamptz
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	registry, err := marketconfig.New([]marketconfig.Config{{
		MarketID: "OM", Locale: "en-US", Currency: "OMR",
		TimeZone: "Asia/Muscat", PolicyVersion: "identity-test-v1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := marketconfig.NewMatrix(registry, []marketconfig.AdapterConfig{{
		MarketID: "OM", Locale: "en-US", PolicyVersion: "identity-test-v1",
		Adapter: "identity.google", Capabilities: []string{"login"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	missing := adapters.VerifiedExternalIdentity{
		Provider: adapters.ProviderGoogle, Subject: "not-linked-subject",
		Nonce: "missing-link-login-nonce", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
	}
	missingDigest := sha256.Sum256([]byte(missing.Nonce))
	if _, err = pool.Exec(ctx, `INSERT INTO external_identity_link_nonces(identity_id,provider,nonce_hash,expires_at) VALUES(NULL,'google',$1,$2)`, missingDigest[:], missing.ExpiresAt); err != nil {
		t.Fatal(err)
	}

	store := &Store{DB: pool}
	_, err = store.ResolveExternalIdentity(ctx, matrix, "OM", "en-US", staticExternalIdentityVerifier{identity: missing}, PostgresIdentityNonceStore{}, "opaque-assertion")
	if !errors.Is(err, ErrExternalIdentityNotFound) {
		t.Fatalf("expected missing provider link, got %v", err)
	}
	var consumed bool
	if err = pool.QueryRow(ctx, `SELECT consumed_at IS NOT NULL FROM external_identity_link_nonces WHERE nonce_hash=$1`, missingDigest[:]).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed {
		t.Fatal("login nonce was consumed before an existing provider link was resolved")
	}

	if _, err = pool.Exec(ctx, `INSERT INTO phone_identities(id,phone_e164) VALUES('identity-1','+96890000000')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO external_identity_links(provider,provider_subject,phone_identity_id) VALUES('google','linked-subject','identity-1')`); err != nil {
		t.Fatal(err)
	}
	linked := adapters.VerifiedExternalIdentity{
		Provider: adapters.ProviderGoogle, Subject: "linked-subject",
		Nonce: "existing-link-login-nonce", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
	}
	linkedDigest := sha256.Sum256([]byte(linked.Nonce))
	if _, err = pool.Exec(ctx, `INSERT INTO external_identity_link_nonces(identity_id,provider,nonce_hash,expires_at) VALUES(NULL,'google',$1,$2)`, linkedDigest[:], linked.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	verified, err := store.ResolveExternalIdentity(ctx, matrix, "OM", "en-US", staticExternalIdentityVerifier{identity: linked}, PostgresIdentityNonceStore{}, "opaque-assertion")
	if err != nil {
		t.Fatalf("existing provider link did not resolve: %v", err)
	}
	if verified.Phone() != "+96890000000" {
		t.Fatalf("unexpected resolved phone identity: %q", verified.Phone())
	}
	if err = pool.QueryRow(ctx, `SELECT consumed_at IS NOT NULL FROM external_identity_link_nonces WHERE nonce_hash=$1`, linkedDigest[:]).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if !consumed {
		t.Fatal("login nonce was not consumed after resolving the existing provider link")
	}
}
