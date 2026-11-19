package marketplace

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPaymentIdempotencyConflict = errors.New("payment idempotency key reused with different request")
	ErrPaymentStateConflict = errors.New("invalid payment operation state transition")
	ErrPaymentOperationNotFound = errors.New("payment operation not found")
	ErrPaymentWebhookConflict = errors.New("webhook event ID reused with different payload")
	ErrProviderOutcomeUnknown = errors.New("payment provider outcome is unknown; reconciliation required")
)

type PaymentRequest struct {
	IdempotencyKey string
	AmountMinor int64
	Currency string
	Reference string
}

type ProviderResult struct {
	Status string
	ExternalReference string
	FailureCode string
}

type PaymentWebhook struct {
	EventID string `json:"event_id"`
	OperationKey string `json:"operation_key"`
	Status string `json:"status"`
	ExternalReference string `json:"external_reference"`
}

type PaymentProvider interface {
	Authorize(context.Context,PaymentRequest) (ProviderResult,error)
	Capture(context.Context,PaymentRequest) (ProviderResult,error)
	Refund(context.Context,PaymentRequest) (ProviderResult,error)
	VerifyWebhook(context.Context,[]byte,string) (PaymentWebhook,error)
}

type FakeProviderMode string

const (
	FakeProviderSuccess FakeProviderMode = "success"
	FakeProviderDecline FakeProviderMode = "decline"
	FakeProviderTimeoutBefore FakeProviderMode = "timeout_before"
	FakeProviderTimeoutAfter FakeProviderMode = "timeout_after"
)

type FakePaymentProvider struct {
	Mode FakeProviderMode
	Secret []byte
	mu sync.Mutex
	results map[string]ProviderResult
	calls int
}

func NewFakePaymentProvider(mode FakeProviderMode,secret []byte) *FakePaymentProvider {
	return &FakePaymentProvider{Mode:mode,Secret:append([]byte(nil),secret...),results:make(map[string]ProviderResult)}
}

func (p *FakePaymentProvider) execute(ctx context.Context,operation string,request PaymentRequest) (ProviderResult,error) {
	if err:=ctx.Err(); err!=nil { return ProviderResult{},err }
	if request.IdempotencyKey=="" || request.AmountMinor<=0 || ValidateAmount(request.AmountMinor,request.Currency)!=nil {
		return ProviderResult{},ErrLedgerInvalidJournal
	}
	key:=operation+":"+request.IdempotencyKey
	p.mu.Lock()
	defer p.mu.Unlock()
	if result,ok:=p.results[key]; ok { return result,nil }
	p.calls++
	if p.Mode==FakeProviderTimeoutBefore { return ProviderResult{},context.DeadlineExceeded }
	result:=ProviderResult{Status:"succeeded",ExternalReference:"fake_"+hex.EncodeToString(sha256.New().Sum([]byte(key)))}
	if p.Mode==FakeProviderDecline {
		result=ProviderResult{Status:"failed",FailureCode:"fake_declined"}
	}
	if p.Mode==FakeProviderTimeoutAfter {
		p.results[key]=result
		return ProviderResult{},ErrProviderOutcomeUnknown
	}
	p.results[key]=result
	return result,nil
}

func (p *FakePaymentProvider) Authorize(ctx context.Context,r PaymentRequest) (ProviderResult,error) {
	return p.execute(ctx,"authorize",r)
}
func (p *FakePaymentProvider) Capture(ctx context.Context,r PaymentRequest) (ProviderResult,error) {
	return p.execute(ctx,"capture",r)
}
func (p *FakePaymentProvider) Refund(ctx context.Context,r PaymentRequest) (ProviderResult,error) {
	return p.execute(ctx,"refund",r)
}
func (p *FakePaymentProvider) Calls() int {
	p.mu.Lock(); defer p.mu.Unlock()
	return p.calls
}
func (p *FakePaymentProvider) SignWebhook(payload []byte) string {
	mac:=hmac.New(sha256.New,p.Secret)
	_,_=mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
func (p *FakePaymentProvider) VerifyWebhook(ctx context.Context,payload []byte,signature string) (PaymentWebhook,error) {
	if err:=ctx.Err(); err!=nil { return PaymentWebhook{},err }
	provided,err:=hex.DecodeString(signature)
	if err!=nil { return PaymentWebhook{},errors.New("invalid webhook signature") }
	expected,err:=hex.DecodeString(p.SignWebhook(payload))
	if err!=nil || len(provided)!=len(expected) || subtle.ConstantTimeCompare(provided,expected)!=1 {
		return PaymentWebhook{},errors.New("invalid webhook signature")
	}
	var event PaymentWebhook
	if err=json.Unmarshal(payload,&event); err!=nil { return PaymentWebhook{},err }
	if event.EventID=="" || event.OperationKey=="" || (event.Status!="succeeded" && event.Status!="failed") {
		return PaymentWebhook{},errors.New("invalid payment webhook payload")
	}
	return event,nil
}

type PaymentOperation struct {
	ID string `json:"id"`
	ProviderCode string `json:"provider_code"`
	OperationType string `json:"operation_type"`
	IdempotencyKey string `json:"idempotency_key"`
	AmountMinor int64 `json:"amount_minor"`
	Currency string `json:"currency"`
	Status string `json:"status"`
	ExternalReference string `json:"external_reference,omitempty"`
	FailureCode string `json:"failure_code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func scanPaymentOperation(row pgx.Row) (PaymentOperation,error) {
	var op PaymentOperation
	err:=row.Scan(&op.ID,&op.ProviderCode,&op.OperationType,&op.IdempotencyKey,&op.AmountMinor,
		&op.Currency,&op.Status,&op.ExternalReference,&op.FailureCode,&op.CreatedAt,&op.UpdatedAt)
	return op,err
}

func (s *Store) BeginPaymentOperation(ctx context.Context,providerCode,operationType,key string,amount int64,currency string) (PaymentOperation,bool,error) {
	providerCode=strings.ToLower(strings.TrimSpace(providerCode))
	operationType=strings.ToLower(strings.TrimSpace(operationType))
	key=strings.TrimSpace(key)
	currency=strings.ToUpper(strings.TrimSpace(currency))
	if len(providerCode)<2 || len(providerCode)>41 || key=="" || len(key)>200 ||
		(operationType!="authorize" && operationType!="capture" && operationType!="refund") ||
		ValidateAmount(amount,currency)!=nil {
		return PaymentOperation{},false,ErrLedgerInvalidJournal
	}
	id,err:=newID()
	if err!=nil { return PaymentOperation{},false,err }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return PaymentOperation{},false,err }
	defer tx.Rollback(ctx)
	op,err:=scanPaymentOperation(tx.QueryRow(ctx,`
		INSERT INTO payment_operations(id,provider_code,operation_type,idempotency_key,amount_minor,currency)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(provider_code,idempotency_key) DO NOTHING
		RETURNING id,provider_code,operation_type,idempotency_key,amount_minor,currency,status,external_reference,failure_code,created_at,updated_at
	`,id,providerCode,operationType,key,amount,currency))
	created:=err==nil
	if errors.Is(err,pgx.ErrNoRows) {
		op,err=scanPaymentOperation(tx.QueryRow(ctx,`
			SELECT id,provider_code,operation_type,idempotency_key,amount_minor,currency,status,external_reference,failure_code,created_at,updated_at
			FROM payment_operations WHERE provider_code=$1 AND idempotency_key=$2
		`,providerCode,key))
		if err!=nil { return PaymentOperation{},false,err }
		if op.OperationType!=operationType || op.AmountMinor!=amount || op.Currency!=currency {
			return PaymentOperation{},false,ErrPaymentIdempotencyConflict
		}
		if err=tx.Commit(ctx); err!=nil { return PaymentOperation{},false,err }
		return op,false,nil
	}
	if err!=nil { return PaymentOperation{},false,err }
	if _,err=tx.Exec(ctx,`INSERT INTO payment_operation_events(id,operation_id,event_type,from_status,to_status) VALUES($1,$2,'created',NULL,'pending')`,id,id); err!=nil {
		return PaymentOperation{},false,err
	}
	if err=tx.Commit(ctx); err!=nil { return PaymentOperation{},false,err }
	return op,created,nil
}

func (s *Store) RecordPaymentOutcome(ctx context.Context,operationID,status,externalReference,failureCode string) (PaymentOperation,error) {
	if status!="succeeded" && status!="failed" && status!="unknown" { return PaymentOperation{},ErrPaymentStateConflict }
	if status=="succeeded" && strings.TrimSpace(externalReference)=="" { return PaymentOperation{},ErrPaymentStateConflict }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return PaymentOperation{},err }
	defer tx.Rollback(ctx)
	op,err:=scanPaymentOperation(tx.QueryRow(ctx,`
		UPDATE payment_operations SET status=$2,external_reference=NULLIF($3,''),failure_code=NULLIF($4,''),updated_at=now()
		WHERE id=$1 AND status='pending'
		RETURNING id,provider_code,operation_type,idempotency_key,amount_minor,currency,status,external_reference,failure_code,created_at,updated_at
	`,operationID,status,externalReference,failureCode))
	if errors.Is(err,pgx.ErrNoRows) {
		op,err=scanPaymentOperation(tx.QueryRow(ctx,`
			SELECT id,provider_code,operation_type,idempotency_key,amount_minor,currency,status,external_reference,failure_code,created_at,updated_at
			FROM payment_operations WHERE id=$1
		`,operationID))
		if errors.Is(err,pgx.ErrNoRows) { return PaymentOperation{},ErrPaymentOperationNotFound }
		if err!=nil { return PaymentOperation{},err }
		if op.Status==status && op.ExternalReference==externalReference && op.FailureCode==failureCode {
			if err=tx.Commit(ctx); err!=nil { return PaymentOperation{},err }
			return op,nil
		}
		return PaymentOperation{},ErrPaymentStateConflict
	}
	if err!=nil { return PaymentOperation{},err }
	if _,err=tx.Exec(ctx,`INSERT INTO payment_operation_events(id,operation_id,event_type,from_status,to_status,details) VALUES($1,$2,'provider_outcome','pending',$3,$4)`,mustID(),operationID,status,failureCode); err!=nil {
		return PaymentOperation{},err
	}
	if err=tx.Commit(ctx); err!=nil { return PaymentOperation{},err }
	return op,nil
}

func (s *Store) ResolveUnknownPayment(ctx context.Context,operationID,status,externalReference,failureCode string) (PaymentOperation,error) {
	if (status!="succeeded" && status!="failed") || (status=="succeeded" && strings.TrimSpace(externalReference)=="") {
		return PaymentOperation{},ErrPaymentStateConflict
	}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return PaymentOperation{},err }
	defer tx.Rollback(ctx)
	op,err:=scanPaymentOperation(tx.QueryRow(ctx,`
		UPDATE payment_operations SET status=$2,external_reference=NULLIF($3,''),failure_code=NULLIF($4,''),updated_at=now()
		WHERE id=$1 AND status='unknown'
		RETURNING id,provider_code,operation_type,idempotency_key,amount_minor,currency,status,external_reference,failure_code,created_at,updated_at
	`,operationID,status,externalReference,failureCode))
	if errors.Is(err,pgx.ErrNoRows) { return PaymentOperation{},ErrPaymentStateConflict }
	if err!=nil { return PaymentOperation{},err }
	if _,err=tx.Exec(ctx,`INSERT INTO payment_operation_events(id,operation_id,event_type,from_status,to_status,details) VALUES($1,$2,'reconciled','unknown',$3,$4)`,mustID(),operationID,status,failureCode); err!=nil {
		return PaymentOperation{},err
	}
	if err=tx.Commit(ctx); err!=nil { return PaymentOperation{},err }
	return op,nil
}

func (s *Store) RecordPaymentWebhook(ctx context.Context,providerCode,eventID string,payload []byte) (bool,error) {
	providerCode=strings.ToLower(strings.TrimSpace(providerCode))
	eventID=strings.TrimSpace(eventID)
	if providerCode=="" || eventID=="" || len(payload)==0 { return false,ErrPaymentStateConflict }
	hash:=sha256.Sum256(payload)
	id,err:=newID()
	if err!=nil { return false,err }
	tag,err:=s.DB.Exec(ctx,`
		INSERT INTO payment_webhook_receipts(id,provider_code,event_id,payload_hash)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(provider_code,event_id) DO NOTHING
	`,id,providerCode,eventID,hash[:])
	if err!=nil { return false,err }
	if tag.RowsAffected()==1 { return false,nil }
	var existing []byte
	err=s.DB.QueryRow(ctx,`SELECT payload_hash FROM payment_webhook_receipts WHERE provider_code=$1 AND event_id=$2`,providerCode,eventID).Scan(&existing)
	if err!=nil { return false,err }
	if len(existing)!=32 || subtle.ConstantTimeCompare(existing,hash[:])!=1 { return false,ErrPaymentWebhookConflict }
	return true,nil
}

func mustID() string {
	id,err:=newID()
	if err!=nil { panic(fmt.Sprintf("generate audit event ID: %v",err)) }
	return id
}
