package marketplace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/adapters"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketconfig"
)

var (
	ErrExternalIdentityInvalid = errors.New("external identity assertion is invalid")
	ErrExternalIdentityLinkedElsewhere = errors.New("external identity is linked to another account")
	ErrExternalIdentityNotFound = errors.New("external identity link not found")
	ErrIdentityNonceReplay = errors.New("identity provider nonce was already used or expired")
)

type IdentityNonceStore interface {
	// Consume must persist and consume the nonce in the caller's transaction.
	Consume(context.Context, pgx.Tx, string, string, string, time.Time) error
}

func validExternalProvider(provider adapters.IdentityProvider) bool {
	switch provider {
	case adapters.ProviderApple, adapters.ProviderGoogle, adapters.ProviderEmail:
		return true
	default:
		return false
	}
}

func validateExternalAssertion(assertion adapters.VerifiedExternalIdentity, now time.Time) error {
	if !validExternalProvider(assertion.Provider) || strings.TrimSpace(assertion.Subject)=="" ||
		strings.TrimSpace(assertion.Nonce)=="" || !assertion.ExpiresAt.After(now) ||
		assertion.IssuedAt.After(now) || !assertion.ExpiresAt.After(assertion.IssuedAt) {
		return ErrExternalIdentityInvalid
	}
	return nil
}

// LinkExternalIdentity links a freshly verified provider assertion to the
// caller's existing phone identity. The nonce is consumed in the same DB
// transaction as the unique provider-subject link.
func (s *Store) LinkExternalIdentity(ctx context.Context, actor Actor, matrix *marketconfig.Matrix, marketID, locale string, verifier adapters.IdentityTokenVerifier, nonceStore IdentityNonceStore, token string) error {
	if !validRole(actor.Role) || strings.TrimSpace(actor.SubjectID)=="" || strings.TrimSpace(actor.TenantID)=="" {
		return ErrForbidden
	}
	if matrix==nil || verifier==nil || nonceStore==nil {return ErrExternalIdentityInvalid}
	assertion,err:=verifier.Verify(token);if err!=nil{return ErrExternalIdentityInvalid}
	if err=validateExternalAssertion(assertion,time.Now());err!=nil{return err}
	adapterName:="identity."+string(assertion.Provider)
	if err=matrix.Require(marketID,locale,adapterName,"link");err!=nil{return err}

	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
	var identityID string
	err=tx.QueryRow(ctx,`SELECT id FROM phone_identities WHERE id=$1 FOR UPDATE`,actor.SubjectID).Scan(&identityID)
	if errors.Is(err,pgx.ErrNoRows){return ErrForbidden};if err!=nil{return err}
	if err=nonceStore.Consume(ctx,tx,identityID,string(assertion.Provider),assertion.Nonce,assertion.ExpiresAt);err!=nil {
		if errors.Is(err,ErrIdentityNonceReplay){return err}
		return fmt.Errorf("consume identity nonce: %w",err)
	}
	_,err=tx.Exec(ctx,`
		INSERT INTO external_identity_links(provider,provider_subject,phone_identity_id,created_at)
		VALUES($1,$2,$3,now()) ON CONFLICT(provider,provider_subject) DO NOTHING
	`,string(assertion.Provider),assertion.Subject,identityID)
	if err!=nil{return err}
	var linkedIdentity string
	err=tx.QueryRow(ctx,`SELECT phone_identity_id FROM external_identity_links WHERE provider=$1 AND provider_subject=$2`,string(assertion.Provider),assertion.Subject).Scan(&linkedIdentity)
	if err!=nil{return err}
	if linkedIdentity!=identityID{return ErrExternalIdentityLinkedElsewhere}
	return tx.Commit(ctx)
}

// ResolveExternalIdentity resolves only an existing provider link. It never
// creates a phone identity or links accounts based on matching email claims.
func (s *Store) ResolveExternalIdentity(ctx context.Context, matrix *marketconfig.Matrix, marketID, locale string, verifier adapters.IdentityTokenVerifier, nonceStore IdentityNonceStore, token string) (VerifiedIdentity,error) {
	if matrix==nil||verifier==nil||nonceStore==nil{return VerifiedIdentity{},ErrExternalIdentityInvalid}
	assertion,err:=verifier.Verify(token);if err!=nil{return VerifiedIdentity{},ErrExternalIdentityInvalid}
	if err=validateExternalAssertion(assertion,time.Now());err!=nil{return VerifiedIdentity{},err}
	if err=matrix.Require(marketID,locale,"identity."+string(assertion.Provider),"login");err!=nil{return VerifiedIdentity{},err}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return VerifiedIdentity{},err};defer tx.Rollback(ctx)
	var identityID,phone string
	err=tx.QueryRow(ctx,`SELECT p.id,p.phone_e164 FROM external_identity_links l JOIN phone_identities p ON p.id=l.phone_identity_id WHERE l.provider=$1 AND l.provider_subject=$2 FOR UPDATE OF l,p`,string(assertion.Provider),assertion.Subject).Scan(&identityID,&phone)
	if errors.Is(err,pgx.ErrNoRows){return VerifiedIdentity{},ErrExternalIdentityNotFound};if err!=nil{return VerifiedIdentity{},err}
	if err=nonceStore.Consume(ctx,tx,"",string(assertion.Provider),assertion.Nonce,assertion.ExpiresAt);err!=nil{return VerifiedIdentity{},err}
	if err=tx.Commit(ctx);err!=nil{return VerifiedIdentity{},err}
	return VerifiedIdentity{phone:phone},nil
}


// PostgresIdentityNonceStore consumes only a pre-issued, unexpired nonce once.
// The nonce itself is never persisted; only its SHA-256 digest is stored.
type PostgresIdentityNonceStore struct{}

func (PostgresIdentityNonceStore) Consume(ctx context.Context, tx pgx.Tx, identityID, provider, nonce string, expiresAt time.Time) error {
	if tx==nil || nonce=="" || !expiresAt.After(time.Now()) {return ErrIdentityNonceReplay}
	digest:=sha256.Sum256([]byte(nonce))
	tag,err:=tx.Exec(ctx,`UPDATE external_identity_link_nonces SET consumed_at=now()
		WHERE COALESCE(identity_id,'')=$1 AND provider=$2 AND nonce_hash=$3 AND consumed_at IS NULL
		AND expires_at>now()`,identityID,provider,digest[:])
	if err!=nil{return err}
	if tag.RowsAffected()!=1{return ErrIdentityNonceReplay}
	return nil
}

// CreateIdentityNonce issues a short-lived nonce for a link (identityID set)
// or login (identityID empty) flow. Callers must bind it to the provider's
// signed nonce claim and never expose the raw value in logs or URLs.
func (s *Store) CreateIdentityNonce(ctx context.Context, identityID string, provider adapters.IdentityProvider, ttl time.Duration) (string,error) {
	if !validExternalProvider(provider) || ttl<time.Minute || ttl>10*time.Minute {return "",ErrExternalIdentityInvalid}
	if identityID!="" {
		var found string
		if err:=s.DB.QueryRow(ctx,`SELECT id FROM phone_identities WHERE id=$1`,identityID).Scan(&found);err!=nil{return "",ErrForbidden}
	}
	var raw [32]byte
	if _,err:=rand.Read(raw[:]);err!=nil{return "",fmt.Errorf("generate identity nonce: %w",err)}
	nonce:=base64.RawURLEncoding.EncodeToString(raw[:])
	digest:=sha256.Sum256([]byte(nonce))
	var owner any
	if identityID!="" {owner=identityID}
	_,err:=s.DB.Exec(ctx,`INSERT INTO external_identity_link_nonces(identity_id,provider,nonce_hash,expires_at)
		VALUES($1,$2,$3,now()+($4 * interval '1 second'))`,owner,string(provider),digest[:],ttl.Seconds())
	if err!=nil{return "",fmt.Errorf("persist identity nonce: %w",err)}
	return nonce,nil
}

// IssueSessionForExternalIdentity issues a normal opaque session only for an
// already linked phone identity; it never creates or merges identities.
func (s *Store) IssueSessionForExternalIdentity(ctx context.Context, identityID string, pepper []byte, accessTTL, refreshTTL time.Duration) (SessionTokens,error) {
	if strings.TrimSpace(identityID)=="" {return SessionTokens{},ErrSessionInvalid}
	var phone string
	if err:=s.DB.QueryRow(ctx,`SELECT phone_e164 FROM phone_identities WHERE id=$1`,identityID).Scan(&phone);err!=nil{return SessionTokens{},ErrSessionInvalid}
	return s.IssueSession(ctx,VerifiedIdentity{phone:phone},pepper,accessTTL,refreshTTL)
}
