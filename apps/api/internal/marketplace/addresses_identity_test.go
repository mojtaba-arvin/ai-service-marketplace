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
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/adapters"
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
