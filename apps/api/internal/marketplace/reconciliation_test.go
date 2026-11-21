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

func TestSettlementReconciliationPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","009_ledger_accounts.sql","010_ledger_idempotency_reversal.sql","011_payment_adapter_contract.sql","012_settlement_reconciliation.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	suffix:=fmt.Sprintf("%d",time.Now().UnixNano())
	createSucceeded:=func(key string,amount int64,currency,external string) PaymentOperation {
		op,_,createErr:=store.BeginPaymentOperation(ctx,"fake","capture",key,amount,currency)
		if createErr!=nil { t.Fatal(createErr) }
		op,outcomeErr:=store.RecordPaymentOutcome(ctx,op.ID,"succeeded",external,"")
		if outcomeErr!=nil { t.Fatal(outcomeErr) }
		return op
	}
	linkedOp:=createSucceeded("settlement-linked-"+suffix,5000,"OMR","provider-ref-"+suffix)
	unlinkedOp:=createSucceeded("settlement-unlinked-"+suffix,700,"OMR","unlinked-ref-"+suffix)
	amountOp:=createSucceeded("settlement-amount-"+suffix,900,"OMR","amount-ref-"+suffix)
	currencyOp:=createSucceeded("settlement-currency-"+suffix,5000,"USD","currency-ref-"+suffix)

	cash,err:=store.CreateLedgerAccount(ctx,"settle_cash_"+suffix,"OMR","debit")
	if err!=nil { t.Fatal(err) }
	payable,err:=store.CreateLedgerAccount(ctx,"settle_payable_"+suffix,"OMR","credit")
	if err!=nil { t.Fatal(err) }
	journal,err:=store.PostJournal(ctx,"settlement-journal-"+suffix,"OMR","Settlement source",[]JournalLine{
		{AccountCode:cash.Code,Side:"debit",AmountMinor:5000,Currency:"OMR"},
		{AccountCode:payable.Code,Side:"credit",AmountMinor:5000,Currency:"OMR"},
	})
	if err!=nil { t.Fatal(err) }
	if err=store.LinkPaymentOperationToJournal(ctx,linkedOp.ID,journal.ID); err!=nil { t.Fatalf("link payment to ledger: %v",err) }
	if err=store.LinkPaymentOperationToJournal(ctx,linkedOp.ID,journal.ID); err!=nil { t.Fatalf("link replay should be idempotent: %v",err) }
	if err=store.LinkPaymentOperationToJournal(ctx,unlinkedOp.ID,journal.ID); !errors.Is(err,ErrPaymentLedgerLinkConflict) {
		t.Fatalf("one journal cannot be linked to another operation: %v",err)
	}

	before:=0
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM ledger_journals`).Scan(&before); err!=nil { t.Fatal(err) }
	start:=time.Now().UTC().Add(-24*time.Hour)
	end:=time.Now().UTC()
	statementRef:="statement-"+suffix
	lines:=[]StatementLine{
		{ExternalReference:linkedOp.ExternalReference,AmountMinor:5000,Currency:"OMR",SettledAt:end},
		{ExternalReference:"missing-ref-"+suffix,AmountMinor:300,Currency:"OMR",SettledAt:end},
		{ExternalReference:unlinkedOp.ExternalReference,AmountMinor:700,Currency:"OMR",SettledAt:end},
		{ExternalReference:amountOp.ExternalReference,AmountMinor:901,Currency:"OMR",SettledAt:end},
		{ExternalReference:currencyOp.ExternalReference,AmountMinor:5000,Currency:"USD",SettledAt:end},
	}
	report,err:=store.ReconcileProviderStatement(ctx,"fake",statementRef,"OMR",start,end,lines)
	if err!=nil { t.Fatalf("reconcile statement: %v",err) }
	if report.Total!=5 || report.Matched!=1 || report.Differences!=4 {
		t.Fatalf("unexpected reconciliation totals: %#v",report)
	}
	statuses:=map[string]int{}
	for _,line:=range report.Lines { statuses[line.Status]++ }
	if statuses["matched"]!=1 || statuses["missing_operation"]!=1 || statuses["missing_ledger"]!=1 ||
		statuses["amount_mismatch"]!=1 || statuses["currency_mismatch"]!=1 {
		t.Fatalf("unexpected difference categories: %#v",statuses)
	}
	reversed:=append([]StatementLine(nil),lines...)
	for i,j:=0,len(reversed)-1;i<j;i,j=i+1,j-1 { reversed[i],reversed[j]=reversed[j],reversed[i] }
	replay,err:=store.ReconcileProviderStatement(ctx,"fake",statementRef,"OMR",start,end,reversed)
	if err!=nil || replay.BatchID!=report.BatchID { t.Fatalf("statement replay: %#v %v",replay,err) }
	changed:=append([]StatementLine(nil),lines...)
	changed[0].AmountMinor++
	if _,err=store.ReconcileProviderStatement(ctx,"fake",statementRef,"OMR",start,end,changed); !errors.Is(err,ErrSettlementStatementConflict) {
		t.Fatalf("changed statement under same reference must fail: %v",err)
	}
	after:=0
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM ledger_journals`).Scan(&after); err!=nil { t.Fatal(err) }
	if before!=after { t.Fatalf("reconciliation must never auto-adjust the ledger: before=%d after=%d",before,after) }
}
