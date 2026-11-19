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
	for _,name:=range []string{"001_walking_skeleton.sql","009_ledger_accounts.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=pool.Exec(ctx,string(migration)); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	suffix:=fmt.Sprintf("%d",time.Now().UnixNano())
	cash,err:=store.CreateLedgerAccount(ctx,"test_cash_"+suffix,"OMR","debit")
	if err!=nil { t.Fatal(err) }
	payable,err:=store.CreateLedgerAccount(ctx,"test_payable_"+suffix,"OMR","credit")
	if err!=nil { t.Fatal(err) }
	journal,err:=store.PostJournal(ctx,"ledger-test-"+suffix,"OMR","Provider payable",[]JournalLine{
		{AccountCode:cash.Code,Side:"debit",AmountMinor:4250,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:4250,Currency:"OMR"},
	})
	if err!=nil { t.Fatalf("post balanced journal: %v",err) }
	var debit,credit int64
	if err=pool.QueryRow(ctx,`SELECT COALESCE(SUM(amount_minor) FILTER(WHERE side='debit'),0),COALESCE(SUM(amount_minor) FILTER(WHERE side='credit'),0) FROM ledger_entries WHERE journal_id=$1`,journal.ID).Scan(&debit,&credit); err!=nil { t.Fatal(err) }
	if debit!=4250 || credit!=4250 { t.Fatalf("journal totals debit=%d credit=%d",debit,credit) }

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
}
