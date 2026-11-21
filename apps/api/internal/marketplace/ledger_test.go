package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDoubleEntryLedgerPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","009_ledger_accounts.sql","010_ledger_idempotency_reversal.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	suffix:=fmt.Sprintf("%d",time.Now().UnixNano())
	cash,err:=store.CreateLedgerAccount(ctx,"test_cash_"+suffix,"OMR","debit")
	if err!=nil { t.Fatal(err) }
	payable,err:=store.CreateLedgerAccount(ctx,"test_payable_"+suffix,"OMR","credit")
	if err!=nil { t.Fatal(err) }
	key:="ledger-test-"+suffix
	lines:=[]JournalLine{
		{AccountCode:cash.Code,Side:"debit",AmountMinor:4250,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:4250,Currency:"OMR"},
	}
	journal,err:=store.PostJournal(ctx,key,"OMR","Provider payable",lines)
	if err!=nil { t.Fatalf("post balanced journal: %v",err) }
	replayed,err:=store.PostJournal(ctx,key,"OMR","Provider payable",[]JournalLine{lines[1],lines[0]})
	if err!=nil || replayed.ID!=journal.ID { t.Fatalf("same payload must replay same journal: %#v %v",replayed,err) }
	if _,err=store.PostJournal(ctx,key,"OMR","Changed payload",[]JournalLine{
		{AccountCode:cash.Code,Side:"debit",AmountMinor:4300,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:4300,Currency:"OMR"},
	}); !errors.Is(err,ErrLedgerIdempotencyConflict) { t.Fatalf("changed payload under same key must fail: %v",err) }

	var debit,credit int64
	if err=pool.QueryRow(ctx,`SELECT COALESCE(SUM(amount_minor) FILTER(WHERE side='debit'),0),COALESCE(SUM(amount_minor) FILTER(WHERE side='credit'),0) FROM ledger_entries WHERE journal_id=$1`,journal.ID).Scan(&debit,&credit); err!=nil { t.Fatal(err) }
	if debit!=4250 || credit!=4250 { t.Fatalf("journal totals debit=%d credit=%d",debit,credit) }

	reversal,err:=store.ReverseJournal(ctx,"reversal-"+suffix,journal.ID,"Correction")
	if err!=nil { t.Fatalf("reverse journal: %v",err) }
	if reversal.ReversalOfID==nil || *reversal.ReversalOfID!=journal.ID { t.Fatalf("reversal linkage missing: %#v",reversal) }
	replayedReversal,err:=store.ReverseJournal(ctx,"reversal-"+suffix,journal.ID,"Correction")
	if err!=nil || replayedReversal.ID!=reversal.ID { t.Fatalf("same reversal request must replay: %#v %v",replayedReversal,err) }
	if _,err=store.ReverseJournal(ctx,"second-reversal-"+suffix,journal.ID,"Second correction"); !errors.Is(err,ErrLedgerAlreadyReversed) {
		t.Fatalf("a journal may be reversed only once: %v",err)
	}
	var originalSide,reversalSide string
	if err=pool.QueryRow(ctx,`SELECT side FROM ledger_entries WHERE journal_id=$1 AND account_code=$2`,journal.ID,cash.Code).Scan(&originalSide); err!=nil { t.Fatal(err) }
	if err=pool.QueryRow(ctx,`SELECT side FROM ledger_entries WHERE journal_id=$1 AND account_code=$2`,reversal.ID,cash.Code).Scan(&reversalSide); err!=nil { t.Fatal(err) }
	if originalSide!="debit" || reversalSide!="credit" { t.Fatalf("reversal did not invert original line: %s -> %s",originalSide,reversalSide) }

	if _,err=store.PostJournal(ctx,"unbalanced-"+suffix,"OMR","Unbalanced",[]JournalLine{
		{AccountCode:cash.Code,Side:"debit",AmountMinor:100,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:99,Currency:"OMR"},
	}); !errors.Is(err,ErrLedgerUnbalanced) { t.Fatalf("unbalanced journal must fail: %v",err) }
	if _,err=store.PostJournal(ctx,"zero-"+suffix,"OMR","Zero",[]JournalLine{
		{AccountCode:cash.Code,Side:"debit",AmountMinor:0,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:0,Currency:"OMR"},
	}); !errors.Is(err,ErrLedgerInvalidJournal) { t.Fatalf("zero-value journal must fail: %v",err) }
	if _,err=store.PostJournal(ctx,"cross-"+suffix,"OMR","Cross currency",[]JournalLine{
		{AccountCode:cash.Code,Side:"debit",AmountMinor:100,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:100,Currency:"USD"},
	}); !errors.Is(err,ErrLedgerInvalidJournal) { t.Fatalf("cross-currency journal must fail: %v",err) }
	if _,err=store.PostJournal(ctx,"unknown-"+suffix,"OMR","Unknown account",[]JournalLine{
		{AccountCode:"unknown_account",Side:"debit",AmountMinor:100,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:100,Currency:"OMR"},
	}); !errors.Is(err,ErrLedgerAccountNotFound) { t.Fatalf("unknown account must fail: %v",err) }
	if err=store.SetLedgerAccountEnabled(ctx,cash.ID,false); err!=nil { t.Fatal(err) }
	if _,err=store.PostJournal(ctx,"disabled-"+suffix,"OMR","Disabled account",[]JournalLine{
		{AccountCode:cash.Code,Side:"debit",AmountMinor:100,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:100,Currency:"OMR"},
	}); !errors.Is(err,ErrLedgerAccountDisabled) { t.Fatalf("disabled account must fail: %v",err) }
	if _,err=pool.Exec(ctx,`UPDATE ledger_entries SET amount_minor=1 WHERE journal_id=$1`,journal.ID); err==nil {
		t.Fatal("posted ledger entries must be immutable")
	}
	if _,err=pool.Exec(ctx,`UPDATE ledger_journals SET description='tampered' WHERE id=$1`,journal.ID); err==nil {
		t.Fatal("posted ledger journals must be immutable")
	}
}
