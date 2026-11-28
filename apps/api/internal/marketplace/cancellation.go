package marketplace

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

var (
	ErrCancellationPolicyUnavailable = errors.New("no effective cancellation policy for order market and state")
	ErrOrderNotFound = errors.New("order not found")
	ErrCancellationIdempotencyConflict = errors.New("cancellation idempotency key reused with different request")
)

type CancellationPolicy struct {
	ID string `json:"id"`
	MarketCode string `json:"market_code"`
	PolicyVersion string `json:"policy_version"`
	ActorRole Role `json:"actor_role"`
	FromStatus string `json:"from_status"`
	CutoffMinutes int `json:"cutoff_minutes"`
	FeeBasisPoints int `json:"fee_basis_points"`
	EffectiveFrom time.Time `json:"effective_from"`
	CreatedAt time.Time `json:"created_at"`
}

type OrderCancellation struct {
	ID string `json:"id"`
	OrderID string `json:"order_id"`
	ActorIdentityID string `json:"actor_identity_id"`
	PolicyVersion string `json:"policy_version"`
	FromStatus string `json:"from_status"`
	Currency string `json:"currency"`
	AmountMinor int64 `json:"amount_minor"`
	FeeMinor int64 `json:"fee_minor"`
	RefundMinor int64 `json:"refund_minor"`
	Reason string `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type cancellationRequestHash struct {
	OrderID string
	ActorIdentityID string
	Reason string
}

func cancellationHash(orderID,actorID,reason string) []byte {
	payload,_:=json.Marshal(cancellationRequestHash{orderID,actorID,reason})
	hash:=sha256.Sum256(payload)
	return hash[:]
}

func scanOrderCancellation(row pgx.Row) (OrderCancellation,[]byte,error) {
	var c OrderCancellation
	var hash []byte
	err:=row.Scan(&c.ID,&c.OrderID,&c.ActorIdentityID,&c.PolicyVersion,&c.FromStatus,&c.Currency,
		&c.AmountMinor,&c.FeeMinor,&c.RefundMinor,&c.Reason,&c.CreatedAt,&hash)
	return c,hash,err
}

func (s *Store) SaveCancellationPolicy(ctx context.Context, actor Actor, policy CancellationPolicy) (CancellationPolicy, error) {
	policy.MarketCode = strings.ToUpper(strings.TrimSpace(policy.MarketCode))
	policy.PolicyVersion = strings.TrimSpace(policy.PolicyVersion)
	if policy.ActorRole != RoleCustomer || (policy.FromStatus != "requested" && policy.FromStatus != "accepted") ||
		policy.PolicyVersion == "" || len(policy.PolicyVersion) > 80 ||
		policy.CutoffMinutes < 0 || policy.CutoffMinutes > 10080 ||
		policy.FeeBasisPoints < 0 || policy.FeeBasisPoints > 10000 || policy.EffectiveFrom.IsZero() {
		return CancellationPolicy{}, ErrInvalidMarket
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return CancellationPolicy{}, err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: actor.TenantID}, ActionPolicyManage); err != nil {
		return CancellationPolicy{}, err
	}
	market, err := loadMarketConfigTx(ctx, tx, policy.MarketCode)
	if err != nil { return CancellationPolicy{}, err }
	if market == nil || !market.Enabled { return CancellationPolicy{}, ErrMarketUnavailable }
	id, err := newID()
	if err != nil { return CancellationPolicy{}, err }
	err = tx.QueryRow(ctx, `
		INSERT INTO cancellation_policies(id,market_code,policy_version,actor_role,from_status,cutoff_minutes,fee_basis_points,effective_from,created_by_identity_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id,market_code,policy_version,actor_role,from_status,cutoff_minutes,fee_basis_points,effective_from,created_at
	`, id, market.Code, policy.PolicyVersion, string(policy.ActorRole), policy.FromStatus, policy.CutoffMinutes,
		policy.FeeBasisPoints, policy.EffectiveFrom, actor.SubjectID).Scan(&policy.ID, &policy.MarketCode, &policy.PolicyVersion,
		&policy.ActorRole, &policy.FromStatus, &policy.CutoffMinutes, &policy.FeeBasisPoints, &policy.EffectiveFrom, &policy.CreatedAt)
	if err != nil { return CancellationPolicy{}, err }
	if err = tx.Commit(ctx); err != nil { return CancellationPolicy{}, err }
	return policy, nil
}

func (s *Store) CancelOrder(ctx context.Context,actor Actor,orderID,idempotencyKey,reason string) (OrderCancellation,error) {
	idempotencyKey=strings.TrimSpace(idempotencyKey)
	reason=strings.TrimSpace(reason)
	if actor.Role!=RoleCustomer || actor.SubjectID=="" || actor.TenantID=="" ||
		orderID=="" || idempotencyKey=="" || len(idempotencyKey)>200 || reason=="" || len(reason)>1000 {
		return OrderCancellation{},ErrForbidden
	}
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionOrderCancel); err!=nil {
		return OrderCancellation{},err
	}
	hash:=cancellationHash(orderID,actor.SubjectID,reason)
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return OrderCancellation{},err }
	defer tx.Rollback(ctx)

	existing,storedHash,err:=scanOrderCancellation(tx.QueryRow(ctx,`
		SELECT c.id,c.order_id,c.actor_identity_id,c.policy_version,c.from_status,c.currency,c.amount_minor,c.fee_minor,c.refund_minor,c.reason,c.created_at,c.request_hash
		FROM order_cancellations c JOIN orders o ON o.id=c.order_id
		WHERE c.actor_identity_id=$1 AND c.idempotency_key=$2 AND o.tenant_id=$3
	`,actor.SubjectID,idempotencyKey,actor.TenantID))
	if err==nil {
		if len(storedHash)!=32 || subtle.ConstantTimeCompare(storedHash,hash)!=1 { return OrderCancellation{},ErrCancellationIdempotencyConflict }
		if err=tx.Commit(ctx); err!=nil { return OrderCancellation{},err }
		return existing,nil
	}
	if !errors.Is(err,pgx.ErrNoRows) { return OrderCancellation{},err }

	var customerID,status,currency,orderTenantID string
	var amount int64
	var scheduledAt *time.Time
	var countryCode *string
	err=tx.QueryRow(ctx,`
		SELECT customer_id,status,currency,amount_minor,scheduled_at,country_code,tenant_id
		FROM orders WHERE id=$1 FOR UPDATE
	`,orderID).Scan(&customerID,&status,&currency,&amount,&scheduledAt,&countryCode,&orderTenantID)
	if errors.Is(err,pgx.ErrNoRows) { return OrderCancellation{},ErrOrderNotFound }
	if err!=nil { return OrderCancellation{},err }
	if customerID!=actor.SubjectID || orderTenantID!=actor.TenantID { return OrderCancellation{},ErrForbidden }

	already,_,err:=scanOrderCancellation(tx.QueryRow(ctx,`
		SELECT id,order_id,actor_identity_id,policy_version,from_status,currency,amount_minor,fee_minor,refund_minor,reason,created_at,request_hash
		FROM order_cancellations WHERE order_id=$1
	`,orderID))
	if err==nil {
		if err=tx.Commit(ctx); err!=nil { return OrderCancellation{},err }
		return already,nil
	}
	if !errors.Is(err,pgx.ErrNoRows) { return OrderCancellation{},err }
	if status=="cancelled" || !CanTransition(status,"cancelled") { return OrderCancellation{},ErrInvalidTransition }
	if countryCode==nil { return OrderCancellation{},ErrCancellationPolicyUnavailable }

	var policyVersion string
	var cutoffMinutes,feeBPS int
	err=tx.QueryRow(ctx,`
		SELECT policy_version,cutoff_minutes,fee_basis_points
		FROM cancellation_policies
		WHERE market_code=$1 AND actor_role='customer' AND from_status=$2 AND effective_from<=now()
		ORDER BY effective_from DESC,policy_version DESC LIMIT 1
	`,*countryCode,status).Scan(&policyVersion,&cutoffMinutes,&feeBPS)
	if errors.Is(err,pgx.ErrNoRows) { return OrderCancellation{},ErrCancellationPolicyUnavailable }
	if err!=nil { return OrderCancellation{},err }

	fee:=int64(0)
	if scheduledAt==nil || scheduledAt.Sub(time.Now().UTC())<time.Duration(cutoffMinutes)*time.Minute {
		fee=(amount/10000)*int64(feeBPS)+((amount%10000)*int64(feeBPS))/10000
	}
	refund:=amount-fee
	id,err:=newID()
	if err!=nil { return OrderCancellation{},err }
	var cancellation OrderCancellation
	err=tx.QueryRow(ctx,`
		INSERT INTO order_cancellations(id,order_id,actor_identity_id,actor_role,idempotency_key,request_hash,
			market_code,policy_version,from_status,currency,amount_minor,fee_minor,refund_minor,reason)
		VALUES($1,$2,$3,'customer',$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id,order_id,actor_identity_id,policy_version,from_status,currency,amount_minor,fee_minor,refund_minor,reason,created_at
	`,id,orderID,actor.SubjectID,idempotencyKey,hash,*countryCode,policyVersion,status,currency,amount,fee,refund,reason).
		Scan(&cancellation.ID,&cancellation.OrderID,&cancellation.ActorIdentityID,&cancellation.PolicyVersion,
			&cancellation.FromStatus,&cancellation.Currency,&cancellation.AmountMinor,&cancellation.FeeMinor,
			&cancellation.RefundMinor,&cancellation.Reason,&cancellation.CreatedAt)
	if err!=nil { return OrderCancellation{},err }
	if _,err=tx.Exec(ctx,`UPDATE orders SET status='cancelled',updated_at=now() WHERE id=$1 AND status=$2`,orderID,status); err!=nil {
		return OrderCancellation{},err
	}
	bookingTag,err:=tx.Exec(ctx,`UPDATE provider_bookings SET status='cancelled',updated_at=now() WHERE order_id=$1 AND status='booked'`,orderID)
	if err!=nil { return OrderCancellation{},err }
	if bookingTag.RowsAffected()>0 {
		bookingPayload,marshalErr:=json.Marshal(map[string]any{"order_id":orderID,"cancellation_id":id,"status":"cancelled"})
		if marshalErr!=nil { return OrderCancellation{},marshalErr }
		eventID,idErr:=newID()
		if idErr!=nil { return OrderCancellation{},idErr }
		if _,err=outbox.AppendDomainEvent(ctx,tx,outbox.DomainEvent{ID:eventID,Type:"provider.booking_cancelled",Version:1,OccurredAt:time.Now().UTC(),TenantID:actor.TenantID,Aggregate:outbox.EventAggregate{Type:"order",ID:orderID},Data:bookingPayload}); err!=nil {
			return OrderCancellation{},err
		}
	}
	payload,marshalErr:=json.Marshal(map[string]any{"order_id":orderID,"from":status,"to":"cancelled","cancellation_id":id})
	if marshalErr!=nil { return OrderCancellation{},marshalErr }
	eventID,err:=newID()
	if err!=nil { return OrderCancellation{},err }
	if _,err=outbox.AppendDomainEvent(ctx,tx,outbox.DomainEvent{ID:eventID,Type:"order.cancelled",Version:1,OccurredAt:time.Now().UTC(),TenantID:actor.TenantID,Aggregate:outbox.EventAggregate{Type:"order",ID:orderID},Data:payload}); err!=nil {
		return OrderCancellation{},err
	}
	if err=tx.Commit(ctx); err!=nil { return OrderCancellation{},err }
	return cancellation,nil
}
