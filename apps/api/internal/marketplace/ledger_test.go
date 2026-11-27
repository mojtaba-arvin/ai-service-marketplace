package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

	// Concurrent identical journal requests must converge on one immutable
	// journal and one pair of ledger entries.
	raceKey := "ledger-race-" + suffix
	type postResult struct {
		journal LedgerJournal
		err     error
	}
	start := make(chan struct{})
	results := make(chan postResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			posted, postErr := store.PostJournal(ctx, raceKey, "OMR", "Concurrent provider payable", lines)
			results <- postResult{journal: posted, err: postErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var raceJournalID string
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent identical journal post failed: %v", result.err)
		}
		if raceJournalID == "" {
			raceJournalID = result.journal.ID
		}
		if result.journal.ID != raceJournalID {
			t.Fatalf("idempotent race returned different journals: %s and %s", raceJournalID, result.journal.ID)
		}
	}
	var raceJournals, raceEntries int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM ledger_journals WHERE idempotency_key=$1`, raceKey).Scan(&raceJournals); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE journal_id=$1`, raceJournalID).Scan(&raceEntries); err != nil {
		t.Fatal(err)
	}
	if raceJournals != 1 || raceEntries != 2 {
		t.Fatalf("concurrent journal idempotency must persist one journal/two entries: journals=%d entries=%d", raceJournals, raceEntries)
	}

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

	// Concurrent reversal requests with distinct idempotency keys must still
	// produce exactly one compensating journal for the original posting.
	// Use fresh enabled accounts because the earlier test disables the cash account.
	reversalCash, err := store.CreateLedgerAccount(ctx, "test_reversal_cash_"+suffix, "OMR", "debit")
	if err != nil {
		t.Fatalf("create reversal cash account: %v", err)
	}
	reversalPayable, err := store.CreateLedgerAccount(ctx, "test_reversal_payable_"+suffix, "OMR", "credit")
	if err != nil {
		t.Fatalf("create reversal payable account: %v", err)
	}
	reversalLines := []JournalLine{
		{AccountCode: reversalCash.Code, Side: "debit", AmountMinor: 4250, Currency: "OMR"},
		{AccountCode: reversalPayable.Code, Side: "credit", AmountMinor: 4250, Currency: "OMR"},
	}
	reversalRaceOriginal, err := store.PostJournal(ctx, "ledger-reversal-race-original-"+suffix, "OMR", "Concurrent reversal original", reversalLines)
	if err != nil {
		t.Fatalf("post original journal for reversal race: %v", err)
	}
	type reversalResult struct {
		journal LedgerJournal
		err     error
	}
	reversalStart := make(chan struct{})
	reversalResults := make(chan reversalResult, 2)
	var reversalWG sync.WaitGroup
	for i := 0; i < 2; i++ {
		reversalWG.Add(1)
		go func(index int) {
			defer reversalWG.Done()
			<-reversalStart
			reversed, reverseErr := store.ReverseJournal(
				ctx,
				fmt.Sprintf("ledger-reversal-race-%s-%d", suffix, index),
				reversalRaceOriginal.ID,
				"Concurrent correction",
			)
			reversalResults <- reversalResult{journal: reversed, err: reverseErr}
		}(i)
	}
	close(reversalStart)
	reversalWG.Wait()
	close(reversalResults)

	var reversalSuccesses, reversalConflicts int
	for result := range reversalResults {
		switch {
		case result.err == nil:
			reversalSuccesses++
		case errors.Is(result.err, ErrLedgerAlreadyReversed):
			reversalConflicts++
		default:
			t.Fatalf("unexpected concurrent reversal result: %v", result.err)
		}
	}
	if reversalSuccesses != 1 || reversalConflicts != 1 {
		t.Fatalf("concurrent reversal must yield one success and one conflict: successes=%d conflicts=%d", reversalSuccesses, reversalConflicts)
	}
	var reversalCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM ledger_journals WHERE reversal_of_id=$1`, reversalRaceOriginal.ID).Scan(&reversalCount); err != nil {
		t.Fatal(err)
	}
	if reversalCount != 1 {
		t.Fatalf("concurrent reversal must persist exactly one compensating journal: got %d", reversalCount)
	}

}
