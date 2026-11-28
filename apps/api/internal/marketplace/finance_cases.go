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
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrFinanceCaseNotFound = errors.New("finance case not found")
	ErrFinanceCaseInput = errors.New("invalid finance case input")
	ErrFinanceCaseConflict = errors.New("finance case idempotency or state conflict")
	ErrFinanceCaseWebhookConflict = errors.New("finance case webhook event ID reused with different payload")
)

const (
	FinanceCasePayout = "payout"
	FinanceCaseRefund = "refund"
	FinanceCaseChargeback = "chargeback"
	FinanceCasePayoutReversal = "payout_reversal"

	FinanceCaseRequested = "requested"
	FinanceCaseAccepted = "accepted"
	FinanceCaseProviderConfirmed = "provider_confirmed"
	FinanceCaseFailed = "failed"
	FinanceCaseReconciled = "reconciled"
)

type FinanceCase struct {
	ID string `json:"id"`
	TenantID string `json:"tenant_id"`
	CaseType string `json:"case_type"`
	OrderID string `json:"order_id"`
	SourcePaymentOperationID string `json:"source_payment_operation_id"`
	SourceJournalID string `json:"source_journal_id"`
	ProviderCode string `json:"provider_code"`
	ProviderIdempotencyKey string `json:"provider_idempotency_key"`
	AmountMinor int64 `json:"amount_minor"`
	Currency string `json:"currency"`
	Status string `json:"status"`
	ProviderOutcome string `json:"provider_outcome"`
	RequestedByIdentityID string `json:"requested_by_identity_id"`
	AcceptedByIdentityID *string `json:"accepted_by_identity_id,omitempty"`
	AcceptanceReason string `json:"acceptance_reason,omitempty"`
	RequestReason string `json:"request_reason"`
	FailureCode string `json:"failure_code,omitempty"`
	ReversalOfCaseID *string `json:"reversal_of_case_id,omitempty"`
	ReconciledJournalID *string `json:"reconciled_journal_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FinanceCaseInput struct {
	CaseType string
	OrderID string
	SourcePaymentOperationID string
	SourceJournalID string
	ProviderCode string
	ProviderIdempotencyKey string
	AmountMinor int64
	Currency string
	ReversalOfCaseID string
	Reason string
}

type FinanceCaseWebhook struct {
	EventID string `json:"event_id"`
	ProviderCode string `json:"provider_code"`
	ProviderIdempotencyKey string `json:"provider_idempotency_key"`
	Outcome string `json:"outcome"` // confirmed or failed; never infer from HTTP response status.
	ProviderReference string `json:"provider_reference,omitempty"`
	AmountMinor int64 `json:"amount_minor"`
	Currency string `json:"currency"`
	FailureCode string `json:"failure_code,omitempty"`
}

type FinanceCaseWebhookVerifier interface {
	VerifyFinanceCaseWebhook(context.Context, []byte, string) (FinanceCaseWebhook, error)
}

// HMACFinanceCaseWebhookVerifier is a deterministic provider-neutral contract
// implementation for tests and adapters. Production secrets must be injected.
type HMACFinanceCaseWebhookVerifier struct { Secret []byte }

func (v HMACFinanceCaseWebhookVerifier) Sign(payload []byte) string {
	mac := hmac.New(sha256.New, v.Secret)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func (v HMACFinanceCaseWebhookVerifier) VerifyFinanceCaseWebhook(ctx context.Context, payload []byte, signature string) (FinanceCaseWebhook, error) {
	if err := ctx.Err(); err != nil { return FinanceCaseWebhook{}, err }
	if len(v.Secret) < 32 || len(payload) == 0 { return FinanceCaseWebhook{}, errors.New("webhook verifier is not configured") }
	provided, err := hex.DecodeString(signature)
	if err != nil { return FinanceCaseWebhook{}, errors.New("invalid finance webhook signature") }
	expected, _ := hex.DecodeString(v.Sign(payload))
	if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
		return FinanceCaseWebhook{}, errors.New("invalid finance webhook signature")
	}
	var event FinanceCaseWebhook
	if err = json.Unmarshal(payload, &event); err != nil { return FinanceCaseWebhook{}, err }
	event.EventID = strings.TrimSpace(event.EventID)
	event.ProviderCode = strings.ToLower(strings.TrimSpace(event.ProviderCode))
	event.ProviderIdempotencyKey = strings.TrimSpace(event.ProviderIdempotencyKey)
	event.Currency = strings.ToUpper(strings.TrimSpace(event.Currency))
	if event.EventID == "" || event.ProviderIdempotencyKey == "" ||
		(event.Outcome != "confirmed" && event.Outcome != "failed") ||
		(event.Outcome == "confirmed" && strings.TrimSpace(event.ProviderReference) == "") ||
		ValidateAmount(event.AmountMinor, event.Currency) != nil ||
		len(event.EventID) > 200 || len(event.ProviderIdempotencyKey) > 200 ||
		len(event.ProviderReference) > 200 || len(event.FailureCode) > 200 ||
		(event.Outcome == "failed" && strings.TrimSpace(event.FailureCode) == "") {
		return FinanceCaseWebhook{}, ErrFinanceCaseInput
	}
	return event, nil
}

func scanFinanceCase(row pgx.Row) (FinanceCase, error) {
	var c FinanceCase
	err := row.Scan(&c.ID,&c.TenantID,&c.CaseType,&c.OrderID,&c.SourcePaymentOperationID,
		&c.SourceJournalID,&c.ProviderCode,&c.ProviderIdempotencyKey,&c.AmountMinor,&c.Currency,
		&c.Status,&c.ProviderOutcome,&c.RequestedByIdentityID,&c.AcceptedByIdentityID,
		&c.AcceptanceReason,&c.RequestReason,&c.FailureCode,&c.ReversalOfCaseID,&c.ReconciledJournalID,
		&c.CreatedAt,&c.UpdatedAt)
	return c, err
}

const financeCaseColumns = `id,tenant_id,case_type,order_id,source_payment_operation_id,source_journal_id,
	provider_code,provider_idempotency_key,amount_minor,currency,status,provider_outcome,
	requested_by_identity_id,accepted_by_identity_id,acceptance_reason,request_reason,failure_code,
	reversal_of_case_id,reconciled_journal_id,created_at,updated_at`

func (s *Store) CreateFinanceCase(ctx context.Context, actor Actor, in FinanceCaseInput) (FinanceCase, bool, error) {
	in.CaseType = strings.ToLower(strings.TrimSpace(in.CaseType))
	in.OrderID = strings.TrimSpace(in.OrderID)
	in.SourcePaymentOperationID = strings.TrimSpace(in.SourcePaymentOperationID)
	in.SourceJournalID = strings.TrimSpace(in.SourceJournalID)
	in.ProviderCode = strings.ToLower(strings.TrimSpace(in.ProviderCode))
	in.ProviderIdempotencyKey = strings.TrimSpace(in.ProviderIdempotencyKey)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.ReversalOfCaseID = strings.TrimSpace(in.ReversalOfCaseID)
	in.Reason = strings.TrimSpace(in.Reason)
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" ||
		(in.CaseType != FinanceCasePayout && in.CaseType != FinanceCaseRefund &&
			in.CaseType != FinanceCaseChargeback && in.CaseType != FinanceCasePayoutReversal) ||
		in.OrderID == "" || in.SourcePaymentOperationID == "" || in.SourceJournalID == "" ||
		len(in.ProviderCode) < 2 || len(in.ProviderCode) > 41 || in.ProviderIdempotencyKey == "" ||
		len(in.ProviderIdempotencyKey) > 200 || ValidateAmount(in.AmountMinor,in.Currency) != nil ||
		in.Reason == "" || len(in.Reason) > 500 ||
		(in.CaseType == FinanceCasePayoutReversal) != (in.ReversalOfCaseID != "") {
		return FinanceCase{}, false, ErrFinanceCaseInput
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return FinanceCase{}, false, err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionFinanceCaseManage); err != nil {
		return FinanceCase{}, false, err
	}

	// The order supplies tenant ownership. Payment and journal references must
	// be explicitly supplied and linked; no identity/amount/time heuristic is used.
	var orderTenant string
	err = tx.QueryRow(ctx,`SELECT tenant_id FROM orders WHERE id=$1 FOR SHARE`,in.OrderID).Scan(&orderTenant)
	if errors.Is(err,pgx.ErrNoRows) || orderTenant != actor.TenantID { return FinanceCase{},false,ErrForbidden }
	if err != nil { return FinanceCase{},false,err }
	var opProvider, opType, opStatus, opCurrency, opTenant, opOrder string
	var opAmount int64
	err = tx.QueryRow(ctx,`SELECT provider_code,operation_type,status,currency,amount_minor,COALESCE(tenant_id,''),COALESCE(order_id,'')
		FROM payment_operations WHERE id=$1 FOR SHARE`,
		in.SourcePaymentOperationID).Scan(&opProvider,&opType,&opStatus,&opCurrency,&opAmount,&opTenant,&opOrder)
	if errors.Is(err,pgx.ErrNoRows) { return FinanceCase{},false,ErrFinanceCaseInput }
	if err != nil { return FinanceCase{},false,err }
	if opProvider != in.ProviderCode || opType != "capture" || opStatus != "succeeded" || opCurrency != in.Currency || opTenant != actor.TenantID || opOrder != in.OrderID || in.AmountMinor > opAmount {
		return FinanceCase{},false,ErrFinanceCaseInput
	}
	var linked bool
	err = tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM payment_ledger_links WHERE operation_id=$1 AND journal_id=$2)`,
		in.SourcePaymentOperationID,in.SourceJournalID).Scan(&linked)
	if err != nil { return FinanceCase{},false,err }
	if !linked { return FinanceCase{},false,ErrFinanceCaseInput }

	if in.CaseType == FinanceCasePayoutReversal {
		var originalTenant, originalType, originalStatus, originalOrder, originalJournal, originalPayment string
		var originalAmount int64
		err = tx.QueryRow(ctx,`SELECT tenant_id,case_type,status,order_id,source_journal_id,source_payment_operation_id,amount_minor
			FROM finance_cases WHERE id=$1 FOR SHARE`,in.ReversalOfCaseID).
			Scan(&originalTenant,&originalType,&originalStatus,&originalOrder,&originalJournal,&originalPayment,&originalAmount)
		if err != nil { return FinanceCase{},false,ErrFinanceCaseInput }
		if originalTenant != actor.TenantID || originalType != FinanceCasePayout ||
			originalStatus != FinanceCaseReconciled || originalOrder != in.OrderID ||
			originalJournal != in.SourceJournalID || originalPayment != in.SourcePaymentOperationID || in.AmountMinor != originalAmount { return FinanceCase{},false,ErrFinanceCaseInput }
	}

	id, err := newID()
	if err != nil { return FinanceCase{},false,err }
	c, err := scanFinanceCase(tx.QueryRow(ctx,`INSERT INTO finance_cases
		(id,tenant_id,case_type,order_id,source_payment_operation_id,source_journal_id,
		 provider_code,provider_idempotency_key,amount_minor,currency,requested_by_identity_id,request_reason,reversal_of_case_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''))
		ON CONFLICT(provider_code,provider_idempotency_key) DO NOTHING
		RETURNING `+financeCaseColumns,
		id,actor.TenantID,in.CaseType,in.OrderID,in.SourcePaymentOperationID,in.SourceJournalID,
		in.ProviderCode,in.ProviderIdempotencyKey,in.AmountMinor,in.Currency,actor.SubjectID,in.Reason,in.ReversalOfCaseID))
	created := err == nil
	if errors.Is(err,pgx.ErrNoRows) {
		c,err = scanFinanceCase(tx.QueryRow(ctx,`SELECT `+financeCaseColumns+`
			FROM finance_cases WHERE provider_code=$1 AND provider_idempotency_key=$2 FOR SHARE`,in.ProviderCode,in.ProviderIdempotencyKey))
		if err != nil { return FinanceCase{},false,err }
		if c.TenantID != actor.TenantID || c.CaseType != in.CaseType || c.OrderID != in.OrderID ||
			c.SourcePaymentOperationID != in.SourcePaymentOperationID || c.SourceJournalID != in.SourceJournalID ||
			c.AmountMinor != in.AmountMinor || c.Currency != in.Currency || c.RequestReason != in.Reason || c.ReversalOfCaseIDValue() != in.ReversalOfCaseID {
			return FinanceCase{},false,ErrPaymentIdempotencyConflict
		}
		if err = tx.Commit(ctx); err != nil { return FinanceCase{},false,err }
		return c,false,nil
	}
	if err != nil { return FinanceCase{},false,err }
	if _,err = tx.Exec(ctx,`INSERT INTO finance_case_events
		(id,case_id,tenant_id,event_type,from_status,to_status,actor_identity_id,reason)
		VALUES($1,$2,$3,'requested',NULL,'requested',$4,$5)`,id,c.ID,actor.TenantID,actor.SubjectID,in.Reason); err != nil {
		return FinanceCase{},false,err
	}
	if err = tx.Commit(ctx); err != nil { return FinanceCase{},false,err }
	return c,created,nil
}

func (c FinanceCase) ReversalOfCaseIDValue() string {
	if c.ReversalOfCaseID == nil { return "" }
	return *c.ReversalOfCaseID
}

func (s *Store) AcceptFinanceCase(ctx context.Context, actor Actor, caseID, reason string) (FinanceCase,error) {
	caseID = strings.TrimSpace(caseID)
	reason = strings.TrimSpace(reason)
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" || caseID == "" || reason == "" || len(reason) > 500 {
		return FinanceCase{},ErrForbidden
	}
	tx,err := s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err != nil { return FinanceCase{},err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionFinanceCaseManage); err != nil {
		return FinanceCase{},err
	}
	c,err := scanFinanceCase(tx.QueryRow(ctx,`SELECT `+financeCaseColumns+`
		FROM finance_cases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`,caseID,actor.TenantID))
	if errors.Is(err,pgx.ErrNoRows) { return FinanceCase{},ErrFinanceCaseNotFound }
	if err != nil { return FinanceCase{},err }
	if c.Status != FinanceCaseRequested || c.RequestedByIdentityID == actor.SubjectID { return FinanceCase{},ErrFinanceCaseConflict }
	eventID,idErr := newID()
	if idErr != nil { return FinanceCase{},idErr }
	_,err = tx.Exec(ctx,`UPDATE finance_cases SET status='accepted',accepted_by_identity_id=$2,
		acceptance_reason=$3,updated_at=now() WHERE id=$1`,caseID,actor.SubjectID,reason)
	if err != nil { return FinanceCase{},err }
	if _,err = tx.Exec(ctx,`INSERT INTO finance_case_events
		(id,case_id,tenant_id,event_type,from_status,to_status,actor_identity_id,reason)
		VALUES($1,$2,$3,'accepted','requested','accepted',$4,$5)`,eventID,caseID,actor.TenantID,actor.SubjectID,reason); err != nil {
		return FinanceCase{},err
	}
	if err = tx.Commit(ctx); err != nil { return FinanceCase{},err }
	c.Status = FinanceCaseAccepted
	c.AcceptedByIdentityID = &actor.SubjectID
	c.AcceptanceReason = reason
	return c,nil
}


func (s *Store) ApplyFinanceCaseWebhook(ctx context.Context, verifier FinanceCaseWebhookVerifier, payload []byte, signature string) (FinanceCase,bool,error) {
	if verifier == nil { return FinanceCase{},false,ErrFinanceCaseInput }
	event,err := verifier.VerifyFinanceCaseWebhook(ctx,payload,signature)
	if err != nil { return FinanceCase{},false,err }
	event.ProviderCode = strings.ToLower(strings.TrimSpace(event.ProviderCode))
	event.EventID = strings.TrimSpace(event.EventID)
	event.ProviderIdempotencyKey = strings.TrimSpace(event.ProviderIdempotencyKey)
	event.Currency = strings.ToUpper(strings.TrimSpace(event.Currency))
	if event.ProviderCode == "" || event.EventID == "" || event.ProviderIdempotencyKey == "" ||
		(event.Outcome != "confirmed" && event.Outcome != "failed") ||
		(event.Outcome == "confirmed" && strings.TrimSpace(event.ProviderReference) == "") ||
		(event.Outcome == "failed" && strings.TrimSpace(event.FailureCode) == "") ||
		len(event.ProviderReference) > 200 || len(event.FailureCode) > 200 ||
		ValidateAmount(event.AmountMinor,event.Currency) != nil { return FinanceCase{},false,ErrFinanceCaseInput }
	hash := sha256.Sum256(payload)
	tx,err := s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err != nil { return FinanceCase{},false,err }
	defer tx.Rollback(ctx)
	c,err := scanFinanceCase(tx.QueryRow(ctx,`SELECT `+financeCaseColumns+`
		FROM finance_cases WHERE provider_code=$1 AND provider_idempotency_key=$2 FOR UPDATE`,
		event.ProviderCode,event.ProviderIdempotencyKey))
	if errors.Is(err,pgx.ErrNoRows) { return FinanceCase{},false,ErrFinanceCaseNotFound }
	if err != nil { return FinanceCase{},false,err }
	if c.AmountMinor != event.AmountMinor || c.Currency != event.Currency { return FinanceCase{},false,ErrFinanceCaseConflict }

	receiptID,err := newID()
	if err != nil { return FinanceCase{},false,err }
	tag,err := tx.Exec(ctx,`INSERT INTO finance_case_webhook_receipts
		(id,provider_code,event_id,case_id,tenant_id,payload_hash)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(provider_code,event_id) DO NOTHING`,
		receiptID,event.ProviderCode,event.EventID,c.ID,c.TenantID,hash[:])
	if err != nil { return FinanceCase{},false,err }
	if tag.RowsAffected() == 0 {
		var oldHash []byte
		err = tx.QueryRow(ctx,`SELECT payload_hash FROM finance_case_webhook_receipts
			WHERE provider_code=$1 AND event_id=$2`,event.ProviderCode,event.EventID).Scan(&oldHash)
		if err != nil { return FinanceCase{},false,err }
		if len(oldHash) != 32 || subtle.ConstantTimeCompare(oldHash,hash[:]) != 1 {
			return FinanceCase{},false,ErrFinanceCaseWebhookConflict
		}
		if err = tx.Commit(ctx); err != nil { return FinanceCase{},false,err }
		return c,true,nil
	}
	if c.Status != FinanceCaseAccepted { return FinanceCase{},false,ErrFinanceCaseConflict }
	next,eventType,providerOutcome := FinanceCaseProviderConfirmed,"provider_confirmed","confirmed"
	if event.Outcome == "failed" {
		next,eventType,providerOutcome = FinanceCaseFailed,"failed","failed"
		if strings.TrimSpace(event.FailureCode) == "" { return FinanceCase{},false,ErrFinanceCaseInput }
	}
	_,err = tx.Exec(ctx,`UPDATE finance_cases SET status=$2,provider_outcome=$3,failure_code=$4,updated_at=now()
		WHERE id=$1`,c.ID,next,providerOutcome,event.FailureCode)
	if err != nil { return FinanceCase{},false,err }
	details,_ := json.Marshal(map[string]string{"provider_reference":event.ProviderReference})
	if _,err = tx.Exec(ctx,`INSERT INTO finance_case_events
		(id,case_id,tenant_id,event_type,from_status,to_status,reason,details)
		VALUES($1,$2,$3,$4,'accepted',$5,'authenticated provider callback',$6::jsonb)`,
		receiptID,c.ID,c.TenantID,eventType,next,details); err != nil { return FinanceCase{},false,err }
	if err = tx.Commit(ctx); err != nil { return FinanceCase{},false,err }
	c.Status = next
	c.ProviderOutcome = providerOutcome
	c.FailureCode = event.FailureCode
	return c,false,nil
}

func recordFinanceCaseReconciliation(ctx context.Context, tx pgx.Tx, c FinanceCase, actor Actor, journalID, outcome string, actualDebit, actualCredit int64, actualCurrency, reason string) error {
	id,err := newID()
	if err != nil { return err }
	_,err = tx.Exec(ctx,`INSERT INTO finance_case_reconciliation_checks
		(id,case_id,tenant_id,journal_id,outcome,expected_amount_minor,expected_currency,
		 actual_debit_minor,actual_credit_minor,actual_currency,actor_identity_id,reason)
		VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,NULLIF($10,''),$11,$12)`,
		id,c.ID,c.TenantID,journalID,outcome,c.AmountMinor,c.Currency,actualDebit,actualCredit,actualCurrency,actor.SubjectID,reason)
	return err
}

func (s *Store) ReconcileFinanceCase(ctx context.Context, actor Actor, caseID, journalID, reason string) (FinanceCase,error) {
	caseID = strings.TrimSpace(caseID)
	journalID = strings.TrimSpace(journalID)
	reason = strings.TrimSpace(reason)
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" || caseID == "" || journalID == "" || reason == "" || len(reason) > 500 {
		return FinanceCase{},ErrFinanceCaseInput
	}
	tx,err := s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err != nil { return FinanceCase{},err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionFinanceCaseManage); err != nil { return FinanceCase{},err }
	c,err := scanFinanceCase(tx.QueryRow(ctx,`SELECT `+financeCaseColumns+`
		FROM finance_cases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`,caseID,actor.TenantID))
	if errors.Is(err,pgx.ErrNoRows) { return FinanceCase{},ErrFinanceCaseNotFound }
	if err != nil { return FinanceCase{},err }
	if c.Status != FinanceCaseProviderConfirmed { return FinanceCase{},ErrFinanceCaseConflict }
	var currency string
	var reversalOf *string
	var debits,credits int64
	err = tx.QueryRow(ctx,`SELECT j.currency,j.reversal_of_id,
		COALESCE(sum(e.amount_minor) FILTER (WHERE e.side='debit'),0),
		COALESCE(sum(e.amount_minor) FILTER (WHERE e.side='credit'),0)
		FROM ledger_journals j JOIN ledger_entries e ON e.journal_id=j.id
		WHERE j.id=$1 GROUP BY j.id,j.currency,j.reversal_of_id`,journalID).Scan(&currency,&reversalOf,&debits,&credits)
	if errors.Is(err,pgx.ErrNoRows) {
		if recErr := recordFinanceCaseReconciliation(ctx,tx,c,actor,journalID,"missing_journal",0,0,"",reason); recErr != nil { return FinanceCase{},recErr }
		if err = tx.Commit(ctx); err != nil { return FinanceCase{},err }
		return FinanceCase{},ErrFinanceCaseConflict
	}
	if err != nil { return FinanceCase{},err }
	if currency != c.Currency || debits != c.AmountMinor || credits != c.AmountMinor {
		outcome := "amount_mismatch"
		if currency != c.Currency { outcome = "currency_mismatch" }
		if recErr := recordFinanceCaseReconciliation(ctx,tx,c,actor,journalID,outcome,debits,credits,currency,reason); recErr != nil { return FinanceCase{},recErr }
		if err = tx.Commit(ctx); err != nil { return FinanceCase{},err }
		return FinanceCase{},ErrFinanceCaseConflict
	}
	if c.CaseType == FinanceCasePayoutReversal {
		var originalJournal string
		err = tx.QueryRow(ctx,`SELECT reconciled_journal_id FROM finance_cases
			WHERE id=$1 AND tenant_id=$2 AND case_type='payout' AND status='reconciled'`,
			c.ReversalOfCaseIDValue(),c.TenantID).Scan(&originalJournal)
		if err != nil || reversalOf == nil || *reversalOf != originalJournal {
			if recErr := recordFinanceCaseReconciliation(ctx,tx,c,actor,journalID,"reversal_mismatch",debits,credits,currency,reason); recErr != nil { return FinanceCase{},recErr }
			if err = tx.Commit(ctx); err != nil { return FinanceCase{},err }
			return FinanceCase{},ErrFinanceCaseConflict
		}
	}
	linkID,err := newID()
	if err != nil { return FinanceCase{},err }
	if _,err = tx.Exec(ctx,`INSERT INTO finance_case_ledger_links
		(id,case_id,journal_id,tenant_id,amount_minor,currency) VALUES($1,$2,$3,$4,$5,$6)`,
		linkID,c.ID,journalID,c.TenantID,c.AmountMinor,c.Currency); err != nil { return FinanceCase{},err }
	if err = recordFinanceCaseReconciliation(ctx,tx,c,actor,journalID,"matched",debits,credits,currency,reason); err != nil { return FinanceCase{},err }
	_,err = tx.Exec(ctx,`UPDATE finance_cases SET status='reconciled',reconciled_journal_id=$2,updated_at=now()
		WHERE id=$1`,c.ID,journalID)
	if err != nil { return FinanceCase{},err }
	eventID,err := newID()
	if err != nil { return FinanceCase{},err }
	if _,err = tx.Exec(ctx,`INSERT INTO finance_case_events
		(id,case_id,tenant_id,event_type,from_status,to_status,actor_identity_id,reason)
		VALUES($1,$2,$3,'reconciled','provider_confirmed','reconciled',$4,$5)`,
		eventID,c.ID,c.TenantID,actor.SubjectID,reason); err != nil { return FinanceCase{},err }
	if err = tx.Commit(ctx); err != nil { return FinanceCase{},err }
	c.Status = FinanceCaseReconciled
	c.ReconciledJournalID = &journalID
	return c,nil
}

func (c FinanceCase) String() string { return fmt.Sprintf("finance case %s (%s)",c.ID,c.Status) }
