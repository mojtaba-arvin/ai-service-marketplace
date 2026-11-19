package marketplace

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPaymentLedgerLinkConflict = errors.New("payment operation is already linked to a different ledger journal")
	ErrSettlementStatementConflict = errors.New("provider statement reference reused with different content")
)

var settlementProviderCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,40}$`)

type StatementLine struct {
	ExternalReference string
	AmountMinor int64
	Currency string
	SettledAt time.Time
}

type SettlementLineResult struct {
	ExternalReference string `json:"external_reference"`
	AmountMinor int64 `json:"amount_minor"`
	Currency string `json:"currency"`
	Status string `json:"status"`
	MatchedOperationID *string `json:"matched_operation_id,omitempty"`
	MatchedJournalID *string `json:"matched_ledger_journal_id,omitempty"`
	Details string `json:"details,omitempty"`
}

type ReconciliationReport struct {
	BatchID string `json:"batch_id"`
	ProviderCode string `json:"provider_code"`
	StatementReference string `json:"statement_reference"`
	Currency string `json:"currency"`
	Total int `json:"total"`
	Matched int `json:"matched"`
	Differences int `json:"differences"`
	Lines []SettlementLineResult `json:"lines"`
}

type statementHashPayload struct {
	ProviderCode string
	StatementReference string
	Currency string
	PeriodStart time.Time
	PeriodEnd time.Time
	Lines []StatementLine
}

func canonicalStatementHash(providerCode, statementReference, currency string, start, end time.Time, lines []StatementLine) []byte {
	canonical := append([]StatementLine(nil), lines...)
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].ExternalReference < canonical[j].ExternalReference })
	payload, _ := json.Marshal(statementHashPayload{providerCode, statementReference, currency, start.UTC(), end.UTC(), canonical})
	hash := sha256.Sum256(payload)
	return hash[:]
}

func (s *Store) LinkPaymentOperationToJournal(ctx context.Context, operationID, journalID string) error {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return err }
	defer tx.Rollback(ctx)

	var amount int64
	var currency, status string
	err = tx.QueryRow(ctx, `SELECT amount_minor,currency,status FROM payment_operations WHERE id=$1 FOR SHARE`, operationID).Scan(&amount, &currency, &status)
	if errors.Is(err, pgx.ErrNoRows) { return ErrPaymentOperationNotFound }
	if err != nil { return err }
	if status != "succeeded" { return ErrPaymentStateConflict }

	var journalCurrency string
	var debits, credits int64
	err = tx.QueryRow(ctx, `
		SELECT j.currency,
			COALESCE(SUM(e.amount_minor) FILTER(WHERE e.side='debit'),0)::bigint,
			COALESCE(SUM(e.amount_minor) FILTER(WHERE e.side='credit'),0)::bigint
		FROM ledger_journals j LEFT JOIN ledger_entries e ON e.journal_id=j.id
		WHERE j.id=$1 GROUP BY j.id,j.currency
	`, journalID).Scan(&journalCurrency, &debits, &credits)
	if errors.Is(err, pgx.ErrNoRows) { return ErrLedgerJournalNotFound }
	if err != nil { return err }
	if journalCurrency != currency || debits != amount || credits != amount { return ErrPaymentLedgerLinkConflict }

	id, err := newID()
	if err != nil { return err }
	tag, err := tx.Exec(ctx, `
		INSERT INTO payment_ledger_links(id,operation_id,journal_id) VALUES($1,$2,$3)
		ON CONFLICT DO NOTHING
	`, id, operationID, journalID)
	if err != nil { return err }
	if tag.RowsAffected() == 0 {
		var existingOperationID, existingJournalID string
		err = tx.QueryRow(ctx, `SELECT operation_id,journal_id FROM payment_ledger_links WHERE operation_id=$1 OR journal_id=$2 LIMIT 1`,
			operationID, journalID).Scan(&existingOperationID, &existingJournalID)
		if err != nil { return err }
		if existingOperationID != operationID || existingJournalID != journalID { return ErrPaymentLedgerLinkConflict }
	}
	return tx.Commit(ctx)
}

func (s *Store) ReconcileProviderStatement(ctx context.Context, providerCode, statementReference, currency string,
	periodStart, periodEnd time.Time, lines []StatementLine) (ReconciliationReport, error) {
	providerCode = strings.ToLower(strings.TrimSpace(providerCode))
	statementReference = strings.TrimSpace(statementReference)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !settlementProviderCodePattern.MatchString(providerCode) || statementReference == "" || len(statementReference) > 200 ||
		ValidateAmount(1, currency) != nil || periodStart.IsZero() || periodEnd.IsZero() || !periodEnd.After(periodStart) ||
		len(lines) == 0 || len(lines) > 10000 {
		return ReconciliationReport{}, ErrLedgerInvalidJournal
	}
	seen := make(map[string]bool, len(lines))
	normalized := make([]StatementLine, len(lines))
	for i, line := range lines {
		line.ExternalReference = strings.TrimSpace(line.ExternalReference)
		line.Currency = strings.ToUpper(strings.TrimSpace(line.Currency))
		if line.ExternalReference == "" || len(line.ExternalReference) > 200 || seen[line.ExternalReference] ||
			ValidateAmount(line.AmountMinor, line.Currency) != nil || line.SettledAt.IsZero() {
			return ReconciliationReport{}, ErrLedgerInvalidJournal
		}
		seen[line.ExternalReference] = true
		normalized[i] = line
	}
	hash := canonicalStatementHash(providerCode, statementReference, currency, periodStart, periodEnd, normalized)

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return ReconciliationReport{}, err }
	defer tx.Rollback(ctx)
	batchID, err := newID()
	if err != nil { return ReconciliationReport{}, err }
	var createdID string
	err = tx.QueryRow(ctx, `
		INSERT INTO settlement_batches(id,provider_code,statement_reference,currency,period_start,period_end,statement_hash)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT(provider_code,statement_reference) DO NOTHING
		RETURNING id
	`, batchID, providerCode, statementReference, currency, periodStart, periodEnd, hash).Scan(&createdID)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash []byte
		err = tx.QueryRow(ctx, `SELECT id,statement_hash FROM settlement_batches WHERE provider_code=$1 AND statement_reference=$2`,
			providerCode, statementReference).Scan(&createdID, &existingHash)
		if err != nil { return ReconciliationReport{}, err }
		if len(existingHash) != 32 || subtle.ConstantTimeCompare(existingHash, hash) != 1 { return ReconciliationReport{}, ErrSettlementStatementConflict }
		report, loadErr := loadReconciliationReport(ctx, tx, createdID)
		if loadErr != nil { return ReconciliationReport{}, loadErr }
		if err = tx.Commit(ctx); err != nil { return ReconciliationReport{}, err }
		return report, nil
	}
	if err != nil { return ReconciliationReport{}, err }

	for _, line := range normalized {
		status, details := "missing_operation", "no successful payment operation matched this external reference"
		var operationID, journalID *string
		var operationAmount int64
		var operationCurrency string
		err = tx.QueryRow(ctx, `
			SELECT id,amount_minor,currency FROM payment_operations
			WHERE provider_code=$1 AND external_reference=$2 AND status='succeeded'
		`, providerCode, line.ExternalReference).Scan(&operationID, &operationAmount, &operationCurrency)
		if err == nil {
			switch {
			case operationCurrency != line.Currency || line.Currency != currency:
				status, details = "currency_mismatch", "statement, operation, or batch currency differs"
			case operationAmount != line.AmountMinor:
				status, details = "amount_mismatch", "statement amount differs from successful payment operation"
			default:
				var linkedJournal string
				err = tx.QueryRow(ctx, `SELECT journal_id FROM payment_ledger_links WHERE operation_id=$1`, *operationID).Scan(&linkedJournal)
				if errors.Is(err, pgx.ErrNoRows) {
					status, details = "missing_ledger", "successful payment operation has no linked ledger journal"
					err = nil
				} else if err == nil {
					var journalCurrency string
					var debitTotal, creditTotal int64
					err = tx.QueryRow(ctx, `
						SELECT j.currency,
							COALESCE(SUM(e.amount_minor) FILTER(WHERE e.side='debit'),0)::bigint,
							COALESCE(SUM(e.amount_minor) FILTER(WHERE e.side='credit'),0)::bigint
						FROM ledger_journals j LEFT JOIN ledger_entries e ON e.journal_id=j.id
						WHERE j.id=$1 GROUP BY j.id,j.currency
					`, linkedJournal).Scan(&journalCurrency, &debitTotal, &creditTotal)
					if err == nil {
						journalID = &linkedJournal
						if journalCurrency != line.Currency || debitTotal != line.AmountMinor || creditTotal != line.AmountMinor {
							status, details = "ledger_mismatch", "linked ledger journal does not match statement amount or currency"
						} else {
							status, details = "matched", "payment operation and balanced ledger journal match statement"
						}
					}
				}
			}
		} else if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		if err != nil { return ReconciliationReport{}, err }
		id, genErr := newID()
		if genErr != nil { return ReconciliationReport{}, genErr }
		_, err = tx.Exec(ctx, `
			INSERT INTO settlement_lines(id,batch_id,external_reference,amount_minor,currency,settled_at,status,matched_operation_id,matched_ledger_journal_id,details)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, id, createdID, line.ExternalReference, line.AmountMinor, line.Currency, line.SettledAt, status, operationID, journalID, details)
		if err != nil { return ReconciliationReport{}, fmt.Errorf("persist reconciliation line: %w", err) }
	}
	report, err := loadReconciliationReport(ctx, tx, createdID)
	if err != nil { return ReconciliationReport{}, err }
	if err = tx.Commit(ctx); err != nil { return ReconciliationReport{}, err }
	return report, nil
}

func loadReconciliationReport(ctx context.Context, tx pgx.Tx, batchID string) (ReconciliationReport, error) {
	var report ReconciliationReport
	err := tx.QueryRow(ctx, `SELECT id,provider_code,statement_reference,currency FROM settlement_batches WHERE id=$1`, batchID).
		Scan(&report.BatchID, &report.ProviderCode, &report.StatementReference, &report.Currency)
	if err != nil { return ReconciliationReport{}, err }
	rows, err := tx.Query(ctx, `
		SELECT external_reference,amount_minor,currency,status,matched_operation_id,matched_ledger_journal_id,details
		FROM settlement_lines WHERE batch_id=$1 ORDER BY external_reference
	`, batchID)
	if err != nil { return ReconciliationReport{}, err }
	defer rows.Close()
	report.Lines = make([]SettlementLineResult, 0)
	for rows.Next() {
		var line SettlementLineResult
		if err = rows.Scan(&line.ExternalReference, &line.AmountMinor, &line.Currency, &line.Status,
			&line.MatchedOperationID, &line.MatchedJournalID, &line.Details); err != nil { return ReconciliationReport{}, err }
		report.Lines = append(report.Lines, line)
		report.Total++
		if line.Status == "matched" { report.Matched++ } else { report.Differences++ }
	}
	if err = rows.Err(); err != nil { return ReconciliationReport{}, err }
	return report, nil
}
