package marketplace

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// MarkFinanceCaseProviderUnknown records an ambiguous timeout without marking
// the case failed. The accepted case and provider idempotency key remain fixed;
// a later authenticated callback can resolve the outcome without a new key.
func (s *Store) MarkFinanceCaseProviderUnknown(ctx context.Context, actor Actor, caseID, reason string) (FinanceCase, error) {
	caseID = strings.TrimSpace(caseID)
	reason = strings.TrimSpace(reason)
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" ||
		caseID == "" || reason == "" || len(reason) > 500 {
		return FinanceCase{}, ErrFinanceCaseInput
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return FinanceCase{}, err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: actor.TenantID}, ActionFinanceCaseManage); err != nil {
		return FinanceCase{}, err
	}
	c, err := scanFinanceCase(tx.QueryRow(ctx, `SELECT `+financeCaseColumns+`
		FROM finance_cases WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, caseID, actor.TenantID))
	if errors.Is(err, pgx.ErrNoRows) { return FinanceCase{}, ErrFinanceCaseNotFound }
	if err != nil { return FinanceCase{}, err }
	if c.Status != FinanceCaseAccepted { return FinanceCase{}, ErrFinanceCaseConflict }
	if c.ProviderOutcome == "unknown" {
		if err = tx.Commit(ctx); err != nil { return FinanceCase{}, err }
		return c, nil
	}
	eventID, err := newID()
	if err != nil { return FinanceCase{}, err }
	if _, err = tx.Exec(ctx, `UPDATE finance_cases SET provider_outcome='unknown',updated_at=now() WHERE id=$1`, caseID); err != nil {
		return FinanceCase{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO finance_case_events
		(id,case_id,tenant_id,event_type,from_status,to_status,actor_identity_id,reason)
		VALUES($1,$2,$3,'provider_unknown','accepted','accepted',$4,$5)`,
		eventID, caseID, actor.TenantID, actor.SubjectID, reason); err != nil {
		return FinanceCase{}, err
	}
	if err = tx.Commit(ctx); err != nil { return FinanceCase{}, err }
	c.ProviderOutcome = "unknown"
	return c, nil
}
