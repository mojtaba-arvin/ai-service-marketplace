package marketplace

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// BindPaymentOperationToOrder records the explicit source attribution required
// by ADR 0006. It is a one-time finance-authorized operation, not a heuristic.
func (s *Store) BindPaymentOperationToOrder(ctx context.Context, actor Actor, operationID, orderID, journalID, reason string) (bool, error) {
	operationID = strings.TrimSpace(operationID)
	orderID = strings.TrimSpace(orderID)
	journalID = strings.TrimSpace(journalID)
	reason = strings.TrimSpace(reason)
	if actor.Role != RoleOperator || actor.SubjectID == "" || actor.TenantID == "" ||
		operationID == "" || orderID == "" || journalID == "" || reason == "" || len(reason) > 500 {
		return false, ErrFinanceCaseInput
	}
	tx,err := s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err != nil { return false,err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx,tx,actor,Resource{TenantID:actor.TenantID},ActionFinanceCaseManage); err != nil {
		return false,err
	}
	var orderTenant,orderCurrency string
	var orderAmount int64
	err = tx.QueryRow(ctx,`SELECT tenant_id,currency,amount_minor FROM orders WHERE id=$1 FOR SHARE`,
		orderID).Scan(&orderTenant,&orderCurrency,&orderAmount)
	if errors.Is(err,pgx.ErrNoRows) || orderTenant != actor.TenantID { return false,ErrForbidden }
	if err != nil { return false,err }

	var provider,status,currency string
	var amount int64
	var tenantID,existingOrder *string
	err = tx.QueryRow(ctx,`SELECT provider_code,status,currency,amount_minor,tenant_id,order_id
		FROM payment_operations WHERE id=$1 FOR UPDATE`,operationID).
		Scan(&provider,&status,&currency,&amount,&tenantID,&existingOrder)
	if errors.Is(err,pgx.ErrNoRows) { return false,ErrFinanceCaseInput }
	if err != nil { return false,err }
	if status != "succeeded" || amount != orderAmount || currency != orderCurrency {
		return false,ErrFinanceCaseInput
	}
	if tenantID != nil || existingOrder != nil {
		if tenantID == nil || existingOrder == nil || *tenantID != actor.TenantID || *existingOrder != orderID {
			return false,ErrFinanceCaseConflict
		}
		var boundJournal string
		err = tx.QueryRow(ctx,`SELECT journal_id FROM payment_operation_order_bindings WHERE operation_id=$1`,
			operationID).Scan(&boundJournal)
		if err != nil || boundJournal != journalID { return false,ErrFinanceCaseConflict }
		if err = tx.Commit(ctx); err != nil { return false,err }
		return false,nil
	}
	var linked bool
	err = tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM payment_ledger_links WHERE operation_id=$1 AND journal_id=$2)`,
		operationID,journalID).Scan(&linked)
	if err != nil { return false,err }
	if !linked { return false,ErrFinanceCaseInput }
	var journalCurrency string
	var debits,credits int64
	err = tx.QueryRow(ctx,`SELECT j.currency,
		COALESCE(sum(e.amount_minor) FILTER (WHERE e.side='debit'),0),
		COALESCE(sum(e.amount_minor) FILTER (WHERE e.side='credit'),0)
		FROM ledger_journals j JOIN ledger_entries e ON e.journal_id=j.id
		WHERE j.id=$1 GROUP BY j.id,j.currency`,journalID).Scan(&journalCurrency,&debits,&credits)
	if err != nil { return false,ErrFinanceCaseInput }
	if journalCurrency != currency || debits != amount || credits != amount { return false,ErrFinanceCaseInput }

	bindingID,err := newID()
	if err != nil { return false,err }
	if _,err = tx.Exec(ctx,`INSERT INTO payment_operation_order_bindings
		(id,operation_id,tenant_id,order_id,journal_id,actor_identity_id,reason)
		VALUES($1,$2,$3,$4,$5,$6,$7)`,bindingID,operationID,actor.TenantID,orderID,journalID,actor.SubjectID,reason); err != nil {
		return false,err
	}
	if _,err = tx.Exec(ctx,`UPDATE payment_operations SET tenant_id=$2,order_id=$3,updated_at=now()
		WHERE id=$1 AND tenant_id IS NULL AND order_id IS NULL`,operationID,actor.TenantID,orderID); err != nil {
		return false,err
	}
	if err = tx.Commit(ctx); err != nil { return false,err }
	return true,nil
}
