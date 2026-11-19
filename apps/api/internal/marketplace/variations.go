package marketplace

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrQuoteVariationNotFound = errors.New("quote variation not found")

type QuoteVariation struct {
	ID string `json:"id"`
	QuoteID string `json:"quote_id"`
	RevisionNo int `json:"revision_no"`
	BaseAmountMinor int64 `json:"base_amount_minor"`
	AmountDeltaMinor int64 `json:"amount_delta_minor"`
	ProposedTotalMinor int64 `json:"proposed_total_minor"`
	ScopeDelta string `json:"scope_delta"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func latestVariationAction(ctx context.Context,tx pgx.Tx,variationID string) (string,error) {
	var action string
	err:=tx.QueryRow(ctx,`SELECT action FROM quote_variation_decisions WHERE variation_id=$1`,variationID).Scan(&action)
	if errors.Is(err,pgx.ErrNoRows) { return "",nil }
	return action,err
}

func (s *Store) ProposeQuoteVariation(ctx context.Context,actor Actor,quoteID,scopeDelta string,amountDelta int64,expiresAt time.Time) (QuoteVariation,error) {
	scopeDelta=strings.TrimSpace(scopeDelta)
	now:=time.Now().UTC()
	if actor.Role!=RoleProfessional || actor.SubjectID=="" || actor.TenantID=="" ||
		quoteID=="" || scopeDelta=="" || len(scopeDelta)>2000 || amountDelta==0 ||
		expiresAt.Before(now.Add(time.Minute)) || expiresAt.After(now.Add(7*24*time.Hour)) {
		return QuoteVariation{},ErrInvalidQuote
	}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return QuoteVariation{},err }
	defer tx.Rollback(ctx)
	var providerIdentity,providerTenant,orderID string
	var currentAmount int64
	err=tx.QueryRow(ctx,`
		SELECT p.identity_id,p.tenant_id,q.order_id,o.amount_minor
		FROM inspection_quotes q JOIN provider_profiles p ON p.id=q.provider_id
		JOIN orders o ON o.id=q.order_id
		WHERE q.id=$1 FOR UPDATE OF q,o
	`,quoteID).Scan(&providerIdentity,&providerTenant,&orderID,&currentAmount)
	if errors.Is(err,pgx.ErrNoRows) { return QuoteVariation{},ErrQuoteNotFound }
	if err!=nil { return QuoteVariation{},err }
	if providerIdentity!=actor.SubjectID { return QuoteVariation{},ErrForbidden }
	if err=Authorize(actor,Resource{TenantID:providerTenant,OwnerID:providerIdentity},ActionQuoteAccept); err!=nil { return QuoteVariation{},err }
	var quoteAction string
	err=tx.QueryRow(ctx,`SELECT action FROM quote_decisions WHERE quote_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`,quoteID).Scan(&quoteAction)
	if err!=nil { return QuoteVariation{},err }
	if quoteAction!="accepted" { return QuoteVariation{},ErrQuoteStateConflict }
	var pending bool
	err=tx.QueryRow(ctx,`
		SELECT EXISTS(SELECT 1 FROM quote_variations v
			WHERE v.quote_id=$1 AND NOT EXISTS(SELECT 1 FROM quote_variation_decisions d WHERE d.variation_id=v.id))
	`,quoteID).Scan(&pending)
	if err!=nil { return QuoteVariation{},err }
	if pending { return QuoteVariation{},ErrQuoteStateConflict }
	if (amountDelta>0 && currentAmount>math.MaxInt64-amountDelta) ||
		(amountDelta<0 && amountDelta<=-currentAmount) {
		return QuoteVariation{},ErrInvalidQuote
	}
	proposed:=currentAmount+amountDelta
	if proposed<=0 { return QuoteVariation{},ErrInvalidQuote }
	var revision int
	if err=tx.QueryRow(ctx,`SELECT COALESCE(MAX(revision_no),0)+1 FROM quote_variations WHERE quote_id=$1`,quoteID).Scan(&revision); err!=nil {
		return QuoteVariation{},err
	}
	id,err:=newID()
	if err!=nil { return QuoteVariation{},err }
	var variation QuoteVariation
	err=tx.QueryRow(ctx,`
		INSERT INTO quote_variations(id,quote_id,revision_no,base_amount_minor,amount_delta_minor,proposed_total_minor,scope_delta,expires_at,created_by_identity_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id,quote_id,revision_no,base_amount_minor,amount_delta_minor,proposed_total_minor,scope_delta,expires_at,created_at
	`,id,quoteID,revision,currentAmount,amountDelta,proposed,scopeDelta,expiresAt,actor.SubjectID).Scan(&variation.ID,&variation.QuoteID,
		&variation.RevisionNo,&variation.BaseAmountMinor,&variation.AmountDeltaMinor,&variation.ProposedTotalMinor,
		&variation.ScopeDelta,&variation.ExpiresAt,&variation.CreatedAt)
	if err!=nil { return QuoteVariation{},err }
	if err=tx.Commit(ctx); err!=nil { return QuoteVariation{},err }
	return variation,nil
}

func (s *Store) ApproveQuoteVariation(ctx context.Context,actor Actor,variationID string) (QuoteDecision,error) {
	if actor.Role!=RoleCustomer || actor.SubjectID=="" || actor.TenantID=="" { return QuoteDecision{},ErrForbidden }
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionQuoteApprove); err!=nil { return QuoteDecision{},err }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return QuoteDecision{},err }
	defer tx.Rollback(ctx)
	var quoteID,orderID,customerID string
	var baseAmount,proposedAmount,currentAmount int64
	var expiresAt time.Time
	err=tx.QueryRow(ctx,`
		SELECT v.quote_id,q.order_id,o.customer_id,v.base_amount_minor,v.proposed_total_minor,o.amount_minor,v.expires_at
		FROM quote_variations v JOIN inspection_quotes q ON q.id=v.quote_id
		JOIN orders o ON o.id=q.order_id
		WHERE v.id=$1 FOR UPDATE OF v,o
	`,variationID).Scan(&quoteID,&orderID,&customerID,&baseAmount,&proposedAmount,&currentAmount,&expiresAt)
	if errors.Is(err,pgx.ErrNoRows) { return QuoteDecision{},ErrQuoteVariationNotFound }
	if err!=nil { return QuoteDecision{},err }
	if customerID!=actor.SubjectID { return QuoteDecision{},ErrForbidden }
	if !time.Now().Before(expiresAt) || currentAmount!=baseAmount { return QuoteDecision{},ErrQuoteStateConflict }
	last,err:=latestVariationAction(ctx,tx,variationID)
	if err!=nil { return QuoteDecision{},err }
	if last!="" { return QuoteDecision{},ErrQuoteStateConflict }
	decisionID,err:=newID()
	if err!=nil { return QuoteDecision{},err }
	var decision QuoteDecision
	err=tx.QueryRow(ctx,`
		INSERT INTO quote_variation_decisions(id,variation_id,actor_identity_id,actor_role,action)
		VALUES($1,$2,$3,'customer','approved')
		RETURNING id,variation_id,actor_identity_id,actor_role,action,reason,created_at
	`,decisionID,variationID,actor.SubjectID).Scan(&decision.ID,&decision.QuoteID,&decision.ActorIdentityID,
		&decision.ActorRole,&decision.Action,&decision.Reason,&decision.CreatedAt)
	if err!=nil { return QuoteDecision{},err }
	var revision int
	if err=tx.QueryRow(ctx,`SELECT COALESCE(MAX(revision_no),0)+1 FROM order_price_revisions WHERE order_id=$1`,orderID).Scan(&revision); err!=nil {
		return QuoteDecision{},err
	}
	revisionID,err:=newID()
	if err!=nil { return QuoteDecision{},err }
	if _,err=tx.Exec(ctx,`
		INSERT INTO order_price_revisions(id,order_id,revision_no,source_type,source_id,previous_amount_minor,new_amount_minor,approved_by_identity_id)
		VALUES($1,$2,$3,'variation',$4,$5,$6,$7)
	`,revisionID,orderID,revision,variationID,baseAmount,proposedAmount,actor.SubjectID); err!=nil {
		return QuoteDecision{},err
	}
	if _,err=tx.Exec(ctx,`UPDATE orders SET amount_minor=$2,updated_at=now() WHERE id=$1`,orderID,proposedAmount); err!=nil {
		return QuoteDecision{},err
	}
	if err=tx.Commit(ctx); err!=nil { return QuoteDecision{},err }
	return decision,nil
}

func (s *Store) RejectQuoteVariation(ctx context.Context,actor Actor,variationID,reason string) (QuoteDecision,error) {
	reason=strings.TrimSpace(reason)
	if actor.Role!=RoleCustomer || actor.SubjectID=="" || actor.TenantID=="" || reason=="" || len(reason)>1000 {
		return QuoteDecision{},ErrForbidden
	}
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionQuoteApprove); err!=nil { return QuoteDecision{},err }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return QuoteDecision{},err }
	defer tx.Rollback(ctx)
	var customerID string
	var expiresAt time.Time
	err=tx.QueryRow(ctx,`
		SELECT o.customer_id,v.expires_at
		FROM quote_variations v JOIN inspection_quotes q ON q.id=v.quote_id
		JOIN orders o ON o.id=q.order_id WHERE v.id=$1 FOR UPDATE OF v
	`,variationID).Scan(&customerID,&expiresAt)
	if errors.Is(err,pgx.ErrNoRows) { return QuoteDecision{},ErrQuoteVariationNotFound }
	if err!=nil { return QuoteDecision{},err }
	if customerID!=actor.SubjectID { return QuoteDecision{},ErrForbidden }
	if !time.Now().Before(expiresAt) { return QuoteDecision{},ErrQuoteStateConflict }
	last,err:=latestVariationAction(ctx,tx,variationID)
	if err!=nil { return QuoteDecision{},err }
	if last!="" { return QuoteDecision{},ErrQuoteStateConflict }
	id,err:=newID()
	if err!=nil { return QuoteDecision{},err }
	var decision QuoteDecision
	err=tx.QueryRow(ctx,`
		INSERT INTO quote_variation_decisions(id,variation_id,actor_identity_id,actor_role,action,reason)
		VALUES($1,$2,$3,'customer','rejected',$4)
		RETURNING id,variation_id,actor_identity_id,actor_role,action,reason,created_at
	`,id,variationID,actor.SubjectID,reason).Scan(&decision.ID,&decision.QuoteID,&decision.ActorIdentityID,
		&decision.ActorRole,&decision.Action,&decision.Reason,&decision.CreatedAt)
	if err!=nil { return QuoteDecision{},err }
	if err=tx.Commit(ctx); err!=nil { return QuoteDecision{},err }
	return decision,nil
}
