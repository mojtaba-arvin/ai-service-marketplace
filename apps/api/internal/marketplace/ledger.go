package marketplace

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrLedgerInvalidJournal = errors.New("invalid ledger journal")
	ErrLedgerUnbalanced = errors.New("ledger journal debits and credits must balance")
	ErrLedgerAccountNotFound = errors.New("ledger account not found for currency")
	ErrLedgerAccountDisabled = errors.New("ledger account is disabled")
	ErrLedgerIdempotencyConflict = errors.New("ledger idempotency key reused with different payload")
	ErrLedgerAlreadyReversed = errors.New("ledger journal has already been reversed")
	ErrLedgerJournalNotFound = errors.New("ledger journal not found")
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
	ReversalOfID *string `json:"reversal_of_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type journalHashPayload struct {
	Currency string `json:"currency"`
	Description string `json:"description"`
	ReversalOf string `json:"reversal_of,omitempty"`
	Lines []JournalLine `json:"lines"`
}

func canonicalJournalHash(currency,description,reversalOf string,lines []JournalLine) []byte {
	canonical:=append([]JournalLine(nil),lines...)
	sort.Slice(canonical,func(i,j int) bool {
		if canonical[i].AccountCode!=canonical[j].AccountCode { return canonical[i].AccountCode<canonical[j].AccountCode }
		if canonical[i].Side!=canonical[j].Side { return canonical[i].Side<canonical[j].Side }
		if canonical[i].AmountMinor!=canonical[j].AmountMinor { return canonical[i].AmountMinor<canonical[j].AmountMinor }
		return canonical[i].Currency<canonical[j].Currency
	})
	payload,_:=json.Marshal(journalHashPayload{Currency:currency,Description:description,ReversalOf:reversalOf,Lines:canonical})
	hash:=sha256.Sum256(payload)
	return hash[:]
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

func validateJournal(currency,description string,lines []JournalLine) (int64,int64,error) {
	currency=strings.ToUpper(strings.TrimSpace(currency))
	if ValidateAmount(1,currency)!=nil || strings.TrimSpace(description)=="" || len(description)>500 || len(lines)<2 || len(lines)>100 {
		return 0,0,ErrLedgerInvalidJournal
	}
	var debits,credits int64
	for _,line:=range lines {
		if !ledgerAccountCodePattern.MatchString(line.AccountCode) || line.AmountMinor<=0 ||
			(line.Side!="debit" && line.Side!="credit") || strings.ToUpper(strings.TrimSpace(line.Currency))!=currency {
			return 0,0,ErrLedgerInvalidJournal
		}
		if line.Side=="debit" {
			if debits>math.MaxInt64-line.AmountMinor { return 0,0,ErrLedgerInvalidJournal }
			debits+=line.AmountMinor
		} else {
			if credits>math.MaxInt64-line.AmountMinor { return 0,0,ErrLedgerInvalidJournal }
			credits+=line.AmountMinor
		}
	}
	if debits==0 || credits==0 || debits!=credits { return debits,credits,ErrLedgerUnbalanced }
	return debits,credits,nil
}

func scanJournal(row pgx.Row) (LedgerJournal,[]byte,error) {
	var journal LedgerJournal
	var hash []byte
	err:=row.Scan(&journal.ID,&journal.IdempotencyKey,&journal.Currency,&journal.Description,&journal.ReversalOfID,&journal.CreatedAt,&hash)
	return journal,hash,err
}

func (s *Store) existingJournalByKey(ctx context.Context,tx pgx.Tx,key string,hash []byte) (LedgerJournal,bool,error) {
	journal,stored,err:=scanJournal(tx.QueryRow(ctx,`
		SELECT id,idempotency_key,currency,description,reversal_of_id,created_at,request_hash
		FROM ledger_journals WHERE idempotency_key=$1
	`,key))
	if errors.Is(err,pgx.ErrNoRows) { return LedgerJournal{},false,nil }
	if err!=nil { return LedgerJournal{},false,err }
	if len(stored)!=32 || subtle.ConstantTimeCompare(stored,hash)!=1 {
		return LedgerJournal{},false,ErrLedgerIdempotencyConflict
	}
	return journal,true,nil
}

func (s *Store) PostJournal(ctx context.Context,idempotencyKey,currency,description string,lines []JournalLine) (LedgerJournal,error) {
	idempotencyKey=strings.TrimSpace(idempotencyKey)
	currency=strings.ToUpper(strings.TrimSpace(currency))
	description=strings.TrimSpace(description)
	if idempotencyKey=="" || len(idempotencyKey)>200 { return LedgerJournal{},ErrLedgerInvalidJournal }
	normalizedLines:=append([]JournalLine(nil),lines...)
	for i:=range normalizedLines {
		if strings.ToUpper(strings.TrimSpace(normalizedLines[i].Currency))!=currency { return LedgerJournal{},ErrLedgerInvalidJournal }
		normalizedLines[i].Currency=currency
	}
	if _,_,err:=validateJournal(currency,description,normalizedLines); err!=nil { return LedgerJournal{},err }
	hash:=canonicalJournalHash(currency,description,"",normalizedLines)

	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return LedgerJournal{},err }
	defer tx.Rollback(ctx)
	if existing,found,lookupErr:=s.existingJournalByKey(ctx,tx,idempotencyKey,hash); lookupErr!=nil {
		return LedgerJournal{},lookupErr
	} else if found {
		if err=tx.Commit(ctx); err!=nil { return LedgerJournal{},err }
		return existing,nil
	}

	checked:=make(map[string]bool,len(lines))
	for _,line:=range normalizedLines {
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
	journal,storedHash,err:=scanJournal(tx.QueryRow(ctx,`
		INSERT INTO ledger_journals(id,idempotency_key,currency,description,request_hash)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(idempotency_key) DO NOTHING
		RETURNING id,idempotency_key,currency,description,reversal_of_id,created_at,request_hash
	`,id,idempotencyKey,currency,description,hash))
	if errors.Is(err,pgx.ErrNoRows) {
		existing,found,lookupErr:=s.existingJournalByKey(ctx,tx,idempotencyKey,hash)
		if lookupErr!=nil { return LedgerJournal{},lookupErr }
		if !found { return LedgerJournal{},ErrLedgerIdempotencyConflict }
		if err=tx.Commit(ctx); err!=nil { return LedgerJournal{},err }
		return existing,nil
	}
	if err!=nil { return LedgerJournal{},fmt.Errorf("create ledger journal: %w",err) }
	if len(storedHash)!=32 { return LedgerJournal{},ErrLedgerIdempotencyConflict }
	for _,line:=range lines {
		if _,err=tx.Exec(ctx,`
			INSERT INTO ledger_entries(journal_id,account_code,side,amount_minor)
			VALUES($1,$2,$3,$4)
		`,journal.ID,line.AccountCode,line.Side,line.AmountMinor); err!=nil {
			return LedgerJournal{},fmt.Errorf("post ledger line: %w",err)
		}
	}
	if err=tx.Commit(ctx); err!=nil { return LedgerJournal{},err }
	return journal,nil
}

func (s *Store) ReverseJournal(ctx context.Context,idempotencyKey,originalJournalID,description string) (LedgerJournal,error) {
	idempotencyKey=strings.TrimSpace(idempotencyKey)
	description=strings.TrimSpace(description)
	if idempotencyKey=="" || len(idempotencyKey)>200 || description=="" || len(description)>500 {
		return LedgerJournal{},ErrLedgerInvalidJournal
	}
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return LedgerJournal{},err }
	defer tx.Rollback(ctx)

	var currency string
	err=tx.QueryRow(ctx,`SELECT currency FROM ledger_journals WHERE id=$1 FOR UPDATE`,originalJournalID).Scan(&currency)
	if errors.Is(err,pgx.ErrNoRows) { return LedgerJournal{},ErrLedgerJournalNotFound }
	if err!=nil { return LedgerJournal{},err }
	rows,err:=tx.Query(ctx,`SELECT account_code,side,amount_minor FROM ledger_entries WHERE journal_id=$1 ORDER BY account_code,side,amount_minor`,originalJournalID)
	if err!=nil { return LedgerJournal{},err }
	lines:=make([]JournalLine,0)
	for rows.Next() {
		var line JournalLine
		if err=rows.Scan(&line.AccountCode,&line.Side,&line.AmountMinor); err!=nil { rows.Close(); return LedgerJournal{},err }
		line.Currency=currency
		if line.Side=="debit" { line.Side="credit" } else { line.Side="debit" }
		lines=append(lines,line)
	}
	if err=rows.Err(); err!=nil { rows.Close(); return LedgerJournal{},err }
	rows.Close()
	if _,_,err=validateJournal(currency,description,lines); err!=nil { return LedgerJournal{},err }
	hash:=canonicalJournalHash(currency,description,originalJournalID,lines)
	if existing,found,lookupErr:=s.existingJournalByKey(ctx,tx,idempotencyKey,hash); lookupErr!=nil {
		return LedgerJournal{},lookupErr
	} else if found {
		if existing.ReversalOfID==nil || *existing.ReversalOfID!=originalJournalID { return LedgerJournal{},ErrLedgerIdempotencyConflict }
		if err=tx.Commit(ctx); err!=nil { return LedgerJournal{},err }
		return existing,nil
	}
	var existingReversal string
	err=tx.QueryRow(ctx,`SELECT id FROM ledger_journals WHERE reversal_of_id=$1`,originalJournalID).Scan(&existingReversal)
	if err==nil { return LedgerJournal{},ErrLedgerAlreadyReversed }
	if !errors.Is(err,pgx.ErrNoRows) { return LedgerJournal{},err }

	id,err:=newID()
	if err!=nil { return LedgerJournal{},err }
	journal,_,err:=scanJournal(tx.QueryRow(ctx,`
		INSERT INTO ledger_journals(id,idempotency_key,currency,description,request_hash,reversal_of_id)
		VALUES($1,$2,$3,$4,$5,$6)
		RETURNING id,idempotency_key,currency,description,reversal_of_id,created_at,request_hash
	`,id,idempotencyKey,currency,description,hash,originalJournalID))
	if err!=nil { return LedgerJournal{},fmt.Errorf("create reversal journal: %w",err) }
	for _,line:=range lines {
		if _,err=tx.Exec(ctx,`
			INSERT INTO ledger_entries(journal_id,account_code,side,amount_minor)
			VALUES($1,$2,$3,$4)
		`,journal.ID,line.AccountCode,line.Side,line.AmountMinor); err!=nil {
			return LedgerJournal{},fmt.Errorf("post reversal line: %w",err)
		}
	}
	if err=tx.Commit(ctx); err!=nil { return LedgerJournal{},err }
	return journal,nil
}
