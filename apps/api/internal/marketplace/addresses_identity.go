package marketplace

import (
	"context"
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
func (s *Store) ResolveExternalIdentity(ctx context.Context, matrix *marketconfig.Matrix, marketID, locale string, verifier adapters.IdentityTokenVerifier, nonceStore IdentityNonceStore, token string) (string,error) {
	if matrix==nil||verifier==nil||nonceStore==nil{return "",ErrExternalIdentityInvalid}
	assertion,err:=verifier.Verify(token);if err!=nil{return "",ErrExternalIdentityInvalid}
	if err=validateExternalAssertion(assertion,time.Now());err!=nil{return "",err}
	if err=matrix.Require(marketID,locale,"identity."+string(assertion.Provider),"login");err!=nil{return "",err}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return "",err};defer tx.Rollback(ctx)
	var identityID string
	err=tx.QueryRow(ctx,`SELECT phone_identity_id FROM external_identity_links WHERE provider=$1 AND provider_subject=$2 FOR UPDATE`,string(assertion.Provider),assertion.Subject).Scan(&identityID)
	if errors.Is(err,pgx.ErrNoRows){return "",ErrExternalIdentityNotFound};if err!=nil{return "",err}
	if err=nonceStore.Consume(ctx,tx,identityID,string(assertion.Provider),assertion.Nonce,assertion.ExpiresAt);err!=nil{return "",err}
	if err=tx.Commit(ctx);err!=nil{return "",err}
	return identityID,nil
}
