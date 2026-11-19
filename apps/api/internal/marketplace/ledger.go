package marketplace

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrLedgerInvalidJournal = errors.New("invalid ledger journal")
	ErrLedgerUnbalanced = errors.New("ledger journal debits and credits must balance")
	ErrLedgerAccountNotFound = errors.New("ledger account not found for currency")
	ErrLedgerAccountDisabled = errors.New("ledger account is disabled")
	ErrLedgerDuplicateKey = errors.New("ledger idempotency key already exists")
)

var ledgerAccountCodePattern=regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,63}$`)

type LedgerAccount struct {
	ID string `json:"id"`
	Code string `json:"code"`
	Currency string `json:"currency"`
	NormalBalance string `json:"normal_balance"`
	Enabled bool `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type JournalLine struct {
	AccountCode string `json:"account_code"`
	Side string `json:"side"`
	AmountMinor int64 `json:"amount_minor"`
	Currency string `json:"currency"`
}

type LedgerJournal struct {
	ID string `json:"id"`
	IdempotencyKey string `json:"idempotency_key"`
	Currency string `json:"currency"`
	Description string `json:"description"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateLedgerAccount(ctx context.Context, code,currency,normalBalance string) (LedgerAccount,error) {
	code=strings.TrimSpace(code)
	currency=strings.ToUpper(strings.TrimSpace(currency))
	normalBalance=strings.ToLower(strings.TrimSpace(normalBalance))
	if !ledgerAccountCodePattern.MatchString(code) || ValidateAmount(1,currency)!=nil ||
		(normalBalance!="debit" && normalBalance!="credit") {
		return LedgerAccount{},ErrLedgerInvalidJournal
	}
	id,err:=newID()
	if err!=nil { return LedgerAccount{},err }
	var account LedgerAccount
	err=s.DB.QueryRow(ctx,`
		INSERT INTO ledger_accounts(id,account_code,currency,normal_balance)
		VALUES($1,$2,$3,$4)
		RETURNING id,account_code,currency,normal_balance,enabled,created_at
	`,id,code,currency,normalBalance).Scan(&account.ID,&account.Code,&account.Currency,
		&account.NormalBalance,&account.Enabled,&account.CreatedAt)
	if err!=nil { return LedgerAccount{},fmt.Errorf("create ledger account: %w",err) }
	return account,nil
}

func (s *Store) SetLedgerAccountEnabled(ctx context.Context, accountID string, enabled bool) error {
	tag,err:=s.DB.Exec(ctx,`UPDATE ledger_accounts SET enabled=$2 WHERE id=$1`,accountID,enabled)
	if err!=nil { return err }
	if tag.RowsAffected()!=1 { return ErrLedgerAccountNotFound }
	return nil
}

// PostJournal writes one single-currency, balanced journal atomically. Amounts
// are always integer minor units; floating-point values are never accepted.
func (s *Store) PostJournal(ctx context.Context, idempotencyKey,currency,description string, lines []JournalLine) (LedgerJournal,error) {
	idempotencyKey=strings.TrimSpace(idempotencyKey)
	currency=strings.ToUpper(strings.TrimSpace(currency))
	description=strings.TrimSpace(description)
	if idempotencyKey=="" || len(idempotencyKey)>200 || description=="" || len(description)>500 ||
		ValidateAmount(1,currency)!=nil || len(lines)<2 || len(lines)>100 {
		return LedgerJournal{},ErrLedgerInvalidJournal
	}
	var debits,credits int64
	for _,line:=range lines {
		if !ledgerAccountCodePattern.MatchString(line.AccountCode) || line.AmountMinor<=0 ||
			(line.Side!="debit" && line.Side!="credit") || strings.ToUpper(strings.TrimSpace(line.Currency))!=currency {
			return LedgerJournal{},ErrLedgerInvalidJournal
		}
		if line.Side=="debit" {
			if debits>math.MaxInt64-line.AmountMinor { return LedgerJournal{},ErrLedgerInvalidJournal }
			debits+=line.AmountMinor
		} else {
			if credits>math.MaxInt64-line.AmountMinor { return LedgerJournal{},ErrLedgerInvalidJournal }
			credits+=line.AmountMinor
		}
	}
	if debits==0 || credits==0 || debits!=credits { return LedgerJournal{},ErrLedgerUnbalanced }

	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return LedgerJournal{},err }
	defer tx.Rollback(ctx)
	checked:=make(map[string]bool,len(lines))
	for _,line:=range lines {
		if checked[line.AccountCode] { continue }
		checked[line.AccountCode]=true
		var enabled bool
		err=tx.QueryRow(ctx,`SELECT enabled FROM ledger_accounts WHERE account_code=$1 AND currency=$2 FOR SHARE`,line.AccountCode,currency).Scan(&enabled)
		if errors.Is(err,pgx.ErrNoRows) { return LedgerJournal{},ErrLedgerAccountNotFound }
		if err!=nil { return LedgerJournal{},err }
		if !enabled { return LedgerJournal{},ErrLedgerAccountDisabled }
	}
	id,err:=newID()
	if err!=nil { return LedgerJournal{},err }
	var journal LedgerJournal
	err=tx.QueryRow(ctx,`
		INSERT INTO ledger_journals(id,idempotency_key,currency,description)
		VALUES($1,$2,$3,$4)
		RETURNING id,idempotency_key,currency,description,created_at
	`,id,idempotencyKey,currency,description).Scan(&journal.ID,&journal.IdempotencyKey,&journal.Currency,&journal.Description,&journal.CreatedAt)
	if err!=nil {
		if strings.Contains(err.Error(),"ledger_journals_idempotency_key_key") { return LedgerJournal{},ErrLedgerDuplicateKey }
		return LedgerJournal{},fmt.Errorf("create ledger journal: %w",err)
	}
	for _,line:=range lines {
		if _,err=tx.Exec(ctx,`
			INSERT INTO ledger_entries(journal_id,account_code,side,amount_minor)
			VALUES($1,$2,$3,$4)
		`,id,line.AccountCode,line.Side,line.AmountMinor); err!=nil {
			return LedgerJournal{},fmt.Errorf("post ledger line: %w",err)
		}
	}
	if err=tx.Commit(ctx); err!=nil { return LedgerJournal{},err }
	return journal,nil
}
