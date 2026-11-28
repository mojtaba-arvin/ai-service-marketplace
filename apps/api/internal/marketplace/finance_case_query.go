package marketplace

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// FinanceCaseProviderQuery is a read-only provider boundary. It must query the
// existing provider idempotency key; it must never submit a new money operation.
type FinanceCaseProviderQuery interface {
	QueryFinanceCase(context.Context, string, string, string, string, int64, string) (FinanceCaseProviderResult, error)
}

type FinanceCaseProviderResult struct {
	ProviderCode string
	ProviderIdempotencyKey string
	Outcome string // confirmed, failed or unknown
	ProviderReference string
	AmountMinor int64
	Currency string
	FailureCode string
}

// ResolveUnknownFinanceCase queries outside the database transaction, then
// applies only a matching authoritative result under a fresh tenant-scoped
// authorization and row lock. A timeout/query error leaves the case unknown.
func (s *Store) ResolveUnknownFinanceCase(ctx context.Context, actor Actor, caseID string, provider FinanceCaseProviderQuery) (FinanceCase,error) {
	caseID = strings.TrimSpace(caseID)
	if provider == nil || actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" || caseID == "" {
		return FinanceCase{},ErrFinanceCaseInput
	}
	tx,err := s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err != nil { return FinanceCase{},err }
	if err = authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionFinanceCaseManage); err != nil {
		_ = tx.Rollback(ctx)
		return FinanceCase{},err
	}
	c,err := scanFinanceCase(tx.QueryRow(ctx,`SELECT `+financeCaseColumns+`
		FROM finance_cases WHERE id=$1 AND tenant_id=$2 FOR SHARE`,caseID,actor.TenantID))
	if errors.Is(err,pgx.ErrNoRows) { _ = tx.Rollback(ctx); return FinanceCase{},ErrFinanceCaseNotFound }
	if err != nil { _ = tx.Rollback(ctx); return FinanceCase{},err }
	if c.Status != FinanceCaseAccepted || c.ProviderOutcome != "unknown" {
		_ = tx.Rollback(ctx)
		return FinanceCase{},ErrFinanceCaseConflict
	}
	if err = tx.Commit(ctx); err != nil { return FinanceCase{},err }

	result,err := provider.QueryFinanceCase(ctx,c.ProviderCode,c.CaseType,c.ProviderIdempotencyKey,c.OrderID,c.AmountMinor,c.Currency)
	if err != nil { return c,err }
	result.ProviderCode = strings.ToLower(strings.TrimSpace(result.ProviderCode))
	result.ProviderIdempotencyKey = strings.TrimSpace(result.ProviderIdempotencyKey)
	result.Currency = strings.ToUpper(strings.TrimSpace(result.Currency))
	if result.ProviderCode != c.ProviderCode || result.ProviderIdempotencyKey != c.ProviderIdempotencyKey ||
		result.AmountMinor != c.AmountMinor || result.Currency != c.Currency ||
		(result.Outcome != "confirmed" && result.Outcome != "failed" && result.Outcome != "unknown") ||
		(result.Outcome == "confirmed" && strings.TrimSpace(result.ProviderReference) == "") ||
		(result.Outcome == "failed" && strings.TrimSpace(result.FailureCode) == "") ||
		len(result.ProviderReference) > 200 || len(result.FailureCode) > 200 {
		return c,ErrFinanceCaseConflict
	}
	if result.Outcome == "unknown" { return c,nil }

	tx,err = s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err != nil { return FinanceCase{},err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionFinanceCaseManage); err != nil {
		return FinanceCase{},err
	}
	current,err := scanFinanceCase(tx.QueryRow(ctx,`SELECT `+financeCaseColumns+`
		FROM finance_cases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`,caseID,actor.TenantID))
	if errors.Is(err,pgx.ErrNoRows) { return FinanceCase{},ErrFinanceCaseNotFound }
	if err != nil { return FinanceCase{},err }
	if current.Status != FinanceCaseAccepted || current.ProviderOutcome != "unknown" {
		return FinanceCase{},ErrFinanceCaseConflict
	}
	next,eventType,outcome := FinanceCaseProviderConfirmed,"provider_confirmed","confirmed"
	if result.Outcome == "failed" { next,eventType,outcome = FinanceCaseFailed,"failed","failed" }
	eventID,err := newID()
	if err != nil { return FinanceCase{},err }
	if _,err = tx.Exec(ctx,`UPDATE finance_cases SET status=$2,provider_outcome=$3,failure_code=$4,updated_at=now()
		WHERE id=$1`,caseID,next,outcome,result.FailureCode); err != nil { return FinanceCase{},err }
	if _,err = tx.Exec(ctx,`INSERT INTO finance_case_events
		(id,case_id,tenant_id,event_type,from_status,to_status,actor_identity_id,reason,details)
		VALUES($1,$2,$3,$4,'accepted',$5,$6,'authoritative provider query',
		jsonb_build_object('provider_reference',$7::text))`,
		eventID,caseID,actor.TenantID,eventType,next,actor.SubjectID,result.ProviderReference); err != nil {
		return FinanceCase{},err
	}
	if err = tx.Commit(ctx); err != nil { return FinanceCase{},err }
	current.Status = next
	current.ProviderOutcome = outcome
	current.FailureCode = result.FailureCode
	return current,nil
}
