package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type financeCaseQueryFake struct {
	result FinanceCaseProviderResult
	keys []string
	err error
}
func (f *financeCaseQueryFake) QueryFinanceCase(ctx context.Context, key string) (FinanceCaseProviderResult,error) {
	f.keys = append(f.keys,key)
	return f.result,f.err
}

func TestFinanceCaseWorkflowPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel := context.WithTimeout(context.Background(),90*time.Second)
	defer cancel()
	admin,err := pgxpool.New(ctx,dsn)
	if err != nil { t.Fatal(err) }
	defer admin.Close()
	schema := "finance_case_test_"+fmt.Sprint(time.Now().UnixNano())
	if _,err = admin.Exec(ctx,"CREATE SCHEMA "+schema); err != nil { t.Fatal(err) }
	defer func(){ _,_ = admin.Exec(context.Background(),"DROP SCHEMA "+schema+" CASCADE") }()
	config,err := pgxpool.ParseConfig(dsn)
	if err != nil { t.Fatal(err) }
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool,err := pgxpool.NewWithConfig(ctx,config)
	if err != nil { t.Fatal(err) }
	defer pool.Close()

	migrations := []string{
		"001_walking_skeleton.sql","002_identity.sql","009_ledger_accounts.sql",
		"010_ledger_idempotency_reversal.sql","011_payment_adapter_contract.sql",
		"012_settlement_reconciliation.sql","022_identity_memberships.sql","023_order_tenant_scope.sql",
		"031_operator_action_grants.sql","032_operator_permission_event_integrity.sql",
		"033_incident_report_intake.sql","034_incident_reviewer_workflow.sql",
		"035_private_incident_evidence.sql","037_finance_case_workflows.sql",
	}
	for _,name := range migrations {
		raw,e := os.ReadFile(filepath.Join("..","..","migrations",name))
		if e != nil { t.Fatalf("read %s: %v",name,e) }
		if _,e = execMigrationScript(ctx,pool,raw); e != nil { t.Fatalf("apply %s: %v",name,e) }
	}

	store := &Store{DB:pool}
	customerID := newTestIdentity(t,ctx,pool)
	requesterID := newTestIdentity(t,ctx,pool)
	approverID := newTestIdentity(t,ctx,pool)
	outsiderID := newTestIdentity(t,ctx,pool)
	tenant := "finance-tenant-"+fmt.Sprint(time.Now().UnixNano())
	for _,identity := range []string{customerID,requesterID,approverID,outsiderID} {
		if _,err = pool.Exec(ctx,`INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
			VALUES($1,$2,$3,$4)`,"member-"+identity,identity,tenant,
			map[string]string{customerID:"customer",requesterID:"operator",approverID:"operator",outsiderID:"operator"}[identity]); err != nil { t.Fatal(err) }
	}
	for _,identity := range []string{requesterID,approverID} {
		grantID := "grant-"+identity
		if _,err = pool.Exec(ctx,`INSERT INTO operator_action_grants
			(id,identity_id,tenant_id,action,granted_by_kind,grant_reason)
			VALUES($1,$2,$3,'finance:case:manage','system_bootstrap','isolated finance integration fixture')`,
			grantID,identity,tenant); err != nil { t.Fatal(err) }
		if _,err = pool.Exec(ctx,`INSERT INTO operator_permission_events
			(grant_id,tenant_id,identity_id,action,event_type,actor_kind,reason,after_state)
			VALUES($1,$2,$3,'finance:case:manage','granted','system_bootstrap',
			'isolated finance integration fixture','{"active":true}'::jsonb)`,grantID,tenant,identity); err != nil { t.Fatal(err) }
	}
	requester := Actor{SubjectID:requesterID,TenantID:tenant,Role:RoleOperator}
	approver := Actor{SubjectID:approverID,TenantID:tenant,Role:RoleOperator}
	outsider := Actor{SubjectID:outsiderID,TenantID:tenant,Role:RoleOperator}

	orderID := "order-"+fmt.Sprint(time.Now().UnixNano())
	if _,err = pool.Exec(ctx,`INSERT INTO orders(id,customer_id,idempotency_key,amount_minor,currency,status,tenant_id)
		VALUES($1,$2,$3,1000,'OMR','requested',$4)`,orderID,customerID,"order-key-"+orderID,1000,tenant); err != nil { t.Fatal(err) }
	if _,err = store.CreateLedgerAccount(ctx,"customer_funds","OMR","debit"); err != nil { t.Fatal(err) }
	if _,err = store.CreateLedgerAccount(ctx,"provider_payable","OMR","credit"); err != nil { t.Fatal(err) }
	sourceJournal,err := store.PostJournal(ctx,"source-"+orderID,"OMR","source payment",
		[]JournalLine{{AccountCode:"customer_funds",Side:"debit",AmountMinor:1000,Currency:"OMR"},
			{AccountCode:"provider_payable",Side:"credit",AmountMinor:1000,Currency:"OMR"}})
	if err != nil { t.Fatal(err) }
	op,created,err := store.BeginPaymentOperation(ctx,"fake","capture","capture-"+orderID,1000,"OMR")
	if err != nil || !created { t.Fatalf("begin capture: %+v %v %v",op,created,err) }
	op,err = store.RecordPaymentOutcome(ctx,op.ID,"succeeded","fake-capture-"+orderID,"")
	if err != nil { t.Fatal(err) }
	if _,err = pool.Exec(ctx,`INSERT INTO payment_ledger_links(id,operation_id,journal_id) VALUES($1,$2,$3)`,
		"link-"+orderID,op.ID,sourceJournal.ID); err != nil { t.Fatal(err) }
	if bound,bindErr := store.BindPaymentOperationToOrder(ctx,requester,op.ID,orderID,sourceJournal.ID,
		"Explicit finance source attribution from verified order and journal"); bindErr != nil || !bound {
		t.Fatalf("explicit payment/order binding: bound=%v err=%v",bound,bindErr)
	}
	if bound,bindErr := store.BindPaymentOperationToOrder(ctx,requester,op.ID,orderID,sourceJournal.ID,
		"Idempotent binding replay"); bindErr != nil || bound {
		t.Fatalf("same source binding must replay without mutation: bound=%v err=%v",bound,bindErr)
	}
	if _,err = pool.Exec(ctx,`UPDATE payment_operations SET order_id='other-order' WHERE id=$1`,op.ID); err == nil {
		t.Fatal("payment/order attribution must be immutable after binding")
	}
	if _,err = pool.Exec(ctx,`UPDATE payment_operation_order_bindings SET reason='tampered' WHERE operation_id=$1`,op.ID); err == nil {
		t.Fatal("payment/order binding evidence must be append-only")
	}

	base := FinanceCaseInput{CaseType:FinanceCasePayout,OrderID:orderID,
		SourcePaymentOperationID:op.ID,SourceJournalID:sourceJournal.ID,ProviderCode:"fake",
		ProviderIdempotencyKey:"payout-"+orderID,AmountMinor:1000,Currency:"OMR",Reason:"Approved payout request"}
	caseRecord,created,err := store.CreateFinanceCase(ctx,requester,base)
	if err != nil || !created || caseRecord.Status != FinanceCaseRequested { t.Fatalf("create case: %+v %v %v",caseRecord,created,err) }
	replay,created,err := store.CreateFinanceCase(ctx,requester,base)
	if err != nil || created || replay.ID != caseRecord.ID { t.Fatalf("idempotent replay: %+v %v %v",replay,created,err) }
	changed := base
	changed.AmountMinor = 900
	if _,_,err = store.CreateFinanceCase(ctx,requester,changed); !errors.Is(err,ErrPaymentIdempotencyConflict) {
		t.Fatalf("changed request must conflict: %v",err)
	}
	if _,err = store.AcceptFinanceCase(ctx,requester,caseRecord.ID,"self approval"); !errors.Is(err,ErrFinanceCaseConflict) {
		t.Fatalf("requester must not self-approve: %v",err)
	}
	if _,err = store.AcceptFinanceCase(ctx,outsider,caseRecord.ID,"unapproved"); !errors.Is(err,ErrForbidden) {
		t.Fatalf("cross-authority denial: %v",err)
	}
	caseRecord,err = store.AcceptFinanceCase(ctx,approver,caseRecord.ID,"Finance approval recorded")
	if err != nil || caseRecord.Status != FinanceCaseAccepted { t.Fatalf("accept: %+v %v",caseRecord,err) }
	caseRecord,err = store.MarkFinanceCaseProviderUnknown(ctx,approver,caseRecord.ID,"Provider request timed out; outcome unknown")
	if err != nil || caseRecord.Status != FinanceCaseAccepted || caseRecord.ProviderOutcome != "unknown" {
		t.Fatalf("unknown must not be failure or confirmation: %+v %v",caseRecord,err)
	}

	verifier := HMACFinanceCaseWebhookVerifier{Secret:[]byte("offline-fake-webhook-secret-at-least-32-bytes")}
	event := FinanceCaseWebhook{EventID:"evt-"+orderID,ProviderCode:"fake",
		ProviderIdempotencyKey:base.ProviderIdempotencyKey,Outcome:"confirmed",
		ProviderReference:"fake-payout-ref",AmountMinor:1000,Currency:"OMR"}
	payload,_ := json.Marshal(event)
	caseRecord,duplicate,err := store.ApplyFinanceCaseWebhook(ctx,verifier,payload,verifier.Sign(payload))
	if err != nil || duplicate || caseRecord.Status != FinanceCaseProviderConfirmed {
		t.Fatalf("authenticated provider confirmation: %+v duplicate=%v err=%v",caseRecord,duplicate,err)
	}
	caseRecord,duplicate,err = store.ApplyFinanceCaseWebhook(ctx,verifier,payload,verifier.Sign(payload))
	if err != nil || !duplicate || caseRecord.Status != FinanceCaseProviderConfirmed {
		t.Fatalf("duplicate webhook: %+v duplicate=%v err=%v",caseRecord,duplicate,err)
	}
	changedEvent := event
	changedEvent.Outcome = "failed"
	changedEvent.ProviderReference = ""
	changedEvent.FailureCode = "late-failure"
	changedPayload,_ := json.Marshal(changedEvent)
	if _,_,err = store.ApplyFinanceCaseWebhook(ctx,verifier,changedPayload,verifier.Sign(changedPayload)); !errors.Is(err,ErrFinanceCaseWebhookConflict) {
		t.Fatalf("same event id with changed body must conflict: %v",err)
	}

	payoutJournal,err := store.PostJournal(ctx,"payout-journal-"+orderID,"OMR","payout settlement",
		[]JournalLine{{AccountCode:"provider_payable",Side:"debit",AmountMinor:1000,Currency:"OMR"},
			{AccountCode:"customer_funds",Side:"credit",AmountMinor:1000,Currency:"OMR"}})
	if err != nil { t.Fatal(err) }
	caseRecord,err = store.ReconcileFinanceCase(ctx,approver,caseRecord.ID,payoutJournal.ID,"Provider statement and journal match")
	if err != nil || caseRecord.Status != FinanceCaseReconciled { t.Fatalf("reconcile payout: %+v %v",caseRecord,err) }

	// A payout reversal is a separate approved case and must point to a
	// compensating journal that reverses the payout journal, not the capture.
	reversalInput := FinanceCaseInput{CaseType:FinanceCasePayoutReversal,OrderID:orderID,
		SourcePaymentOperationID:op.ID,SourceJournalID:sourceJournal.ID,ProviderCode:"fake",
		ProviderIdempotencyKey:"payout-reversal-"+orderID,AmountMinor:1000,Currency:"OMR",
		ReversalOfCaseID:caseRecord.ID,Reason:"Approved payout reversal"}
	reversal,_,err := store.CreateFinanceCase(ctx,requester,reversalInput)
	if err != nil { t.Fatalf("create payout reversal: %v",err) }
	reversal,err = store.AcceptFinanceCase(ctx,approver,reversal.ID,"Approve reversal")
	if err != nil { t.Fatal(err) }
	reversalEvent := FinanceCaseWebhook{EventID:"evt-reversal-"+orderID,ProviderCode:"fake",
		ProviderIdempotencyKey:reversalInput.ProviderIdempotencyKey,Outcome:"confirmed",
		ProviderReference:"fake-reversal-ref",AmountMinor:1000,Currency:"OMR"}
	reversalPayload,_ := json.Marshal(reversalEvent)
	reversal,_,err = store.ApplyFinanceCaseWebhook(ctx,verifier,reversalPayload,verifier.Sign(reversalPayload))
	if err != nil { t.Fatal(err) }
	compensation,err := store.ReverseJournal(ctx,"reverse-payout-"+orderID,payoutJournal.ID,"Payout reversal")
	if err != nil { t.Fatal(err) }
	reversal,err = store.ReconcileFinanceCase(ctx,approver,reversal.ID,compensation.ID,"Compensating journal confirmed")
	if err != nil || reversal.Status != FinanceCaseReconciled { t.Fatalf("reconcile reversal: %+v %v",reversal,err) }

	// Partial provider failure is a terminal failed case and never links a journal.
	refundInput := FinanceCaseInput{CaseType:FinanceCaseRefund,OrderID:orderID,
		SourcePaymentOperationID:op.ID,SourceJournalID:sourceJournal.ID,ProviderCode:"fake",
		ProviderIdempotencyKey:"refund-failed-"+orderID,AmountMinor:250,Currency:"OMR",Reason:"Refund request"}
	refund,_,err := store.CreateFinanceCase(ctx,requester,refundInput)
	if err != nil { t.Fatal(err) }
	refund,err = store.AcceptFinanceCase(ctx,approver,refund.ID,"Approve refund request")
	if err != nil { t.Fatal(err) }
	failedEvent := FinanceCaseWebhook{EventID:"evt-refund-failed-"+orderID,ProviderCode:"fake",
		ProviderIdempotencyKey:refundInput.ProviderIdempotencyKey,Outcome:"failed",
		FailureCode:"provider_declined",AmountMinor:250,Currency:"OMR"}
	failedPayload,_ := json.Marshal(failedEvent)
	refund,_,err = store.ApplyFinanceCaseWebhook(ctx,verifier,failedPayload,verifier.Sign(failedPayload))
	if err != nil || refund.Status != FinanceCaseFailed { t.Fatalf("failed provider operation: %+v %v",refund,err) }
	var links int
	if err = pool.QueryRow(ctx,`SELECT count(*) FROM finance_case_ledger_links WHERE case_id=$1`,refund.ID).Scan(&links); err != nil { t.Fatal(err) }
	if links != 0 { t.Fatal("failed provider operation must not have a ledger link") }

	// Timeout-after-effect is recovered by a read-only query using the exact
	// original provider idempotency key; an unknown query result changes nothing.
	queryInput := FinanceCaseInput{CaseType:FinanceCaseRefund,OrderID:orderID,
		SourcePaymentOperationID:op.ID,SourceJournalID:sourceJournal.ID,ProviderCode:"fake",
		ProviderIdempotencyKey:"refund-query-"+orderID,AmountMinor:200,Currency:"OMR",Reason:"Query recovery test"}
	queryCase,_,err := store.CreateFinanceCase(ctx,requester,queryInput)
	if err != nil { t.Fatal(err) }
	queryCase,err = store.AcceptFinanceCase(ctx,approver,queryCase.ID,"Approve query recovery test")
	if err != nil { t.Fatal(err) }
	queryCase,err = store.MarkFinanceCaseProviderUnknown(ctx,approver,queryCase.ID,"Request timed out after possible provider effect")
	if err != nil { t.Fatal(err) }
	fakeQuery := &financeCaseQueryFake{result:FinanceCaseProviderResult{
		ProviderCode:"fake",ProviderIdempotencyKey:queryInput.ProviderIdempotencyKey,
		Outcome:"unknown",AmountMinor:200,Currency:"OMR"}}
	queryCase,err = store.ResolveUnknownFinanceCase(ctx,approver,queryCase.ID,fakeQuery)
	if err != nil || queryCase.Status != FinanceCaseAccepted || queryCase.ProviderOutcome != "unknown" {
		t.Fatalf("unknown provider query must preserve unknown state: %+v %v",queryCase,err)
	}
	fakeQuery.result.Outcome = "confirmed"
	fakeQuery.result.ProviderReference = "fake-refund-query-ref"
	queryCase,err = store.ResolveUnknownFinanceCase(ctx,approver,queryCase.ID,fakeQuery)
	if err != nil || queryCase.Status != FinanceCaseProviderConfirmed {
		t.Fatalf("authoritative query recovery: %+v %v",queryCase,err)
	}
	if len(fakeQuery.keys) != 2 || fakeQuery.keys[0] != queryInput.ProviderIdempotencyKey ||
		fakeQuery.keys[1] != queryInput.ProviderIdempotencyKey {
		t.Fatalf("provider query must reuse the original key: %#v",fakeQuery.keys)
	}

	// A race between distinct provider outcomes for one chargeback has exactly
	// one winner; the late/out-of-order outcome cannot regress the terminal case.
	chargeInput := FinanceCaseInput{CaseType:FinanceCaseChargeback,OrderID:orderID,
		SourcePaymentOperationID:op.ID,SourceJournalID:sourceJournal.ID,ProviderCode:"fake",
		ProviderIdempotencyKey:"chargeback-race-"+orderID,AmountMinor:300,Currency:"OMR",Reason:"Provider dispute opened"}
	charge,_,err := store.CreateFinanceCase(ctx,requester,chargeInput)
	if err != nil { t.Fatal(err) }
	charge,err = store.AcceptFinanceCase(ctx,approver,charge.ID,"Acknowledge provider dispute case")
	if err != nil { t.Fatal(err) }
	events := []FinanceCaseWebhook{
		{EventID:"charge-confirmed-"+orderID,ProviderCode:"fake",ProviderIdempotencyKey:chargeInput.ProviderIdempotencyKey,
			Outcome:"confirmed",ProviderReference:"fake-chargeback-ref",AmountMinor:300,Currency:"OMR"},
		{EventID:"charge-failed-"+orderID,ProviderCode:"fake",ProviderIdempotencyKey:chargeInput.ProviderIdempotencyKey,
			Outcome:"failed",FailureCode:"provider_closed",AmountMinor:300,Currency:"OMR"},
	}
	var wg sync.WaitGroup
	results := make(chan error,2)
	for _,e := range events {
		wg.Add(1)
		go func(event FinanceCaseWebhook) {
			defer wg.Done()
			body,_ := json.Marshal(event)
			_,_,applyErr := store.ApplyFinanceCaseWebhook(ctx,verifier,body,verifier.Sign(body))
			results <- applyErr
		}(e)
	}
	wg.Wait(); close(results)
	successes := 0
	for applyErr := range results { if applyErr == nil { successes++ } }
	if successes != 1 { t.Fatalf("chargeback race must have one terminal winner, got %d",successes) }

	// A confirmed chargeback with a mismatched journal is recorded as a durable
	// discrepancy, not silently linked or used to mutate the ledger.
	mismatchInput := chargeInput
	mismatchInput.ProviderIdempotencyKey = "chargeback-mismatch-"+orderID
	mismatch,_,err := store.CreateFinanceCase(ctx,requester,mismatchInput)
	if err != nil { t.Fatal(err) }
	mismatch,err = store.AcceptFinanceCase(ctx,approver,mismatch.ID,"Review separate provider dispute")
	if err != nil { t.Fatal(err) }
	mismatchEvent := FinanceCaseWebhook{EventID:"evt-chargeback-mismatch-"+orderID,ProviderCode:"fake",
		ProviderIdempotencyKey:mismatchInput.ProviderIdempotencyKey,Outcome:"confirmed",
		ProviderReference:"fake-chargeback-mismatch-ref",AmountMinor:300,Currency:"OMR"}
	mismatchPayload,_ := json.Marshal(mismatchEvent)
	mismatch,_,err = store.ApplyFinanceCaseWebhook(ctx,verifier,mismatchPayload,verifier.Sign(mismatchPayload))
	if err != nil || mismatch.Status != FinanceCaseProviderConfirmed { t.Fatalf("confirm discrepancy case: %+v %v",mismatch,err) }

	// A confirmed chargeback with a mismatched journal is recorded as a durable
	// discrepancy, not silently linked or used to mutate the ledger.
	var current FinanceCase
	if err = pool.QueryRow(ctx,`SELECT `+financeCaseColumns+` FROM finance_cases WHERE id=$1`,mismatch.ID).Scan(
		&current.ID,&current.TenantID,&current.CaseType,&current.OrderID,&current.SourcePaymentOperationID,
		&current.SourceJournalID,&current.ProviderCode,&current.ProviderIdempotencyKey,&current.AmountMinor,&current.Currency,
		&current.Status,&current.ProviderOutcome,&current.RequestedByIdentityID,&current.AcceptedByIdentityID,
		&current.AcceptanceReason,&current.RequestReason,&current.FailureCode,&current.ReversalOfCaseID,
		&current.ReconciledJournalID,&current.CreatedAt,&current.UpdatedAt); err != nil { t.Fatal(err) }
	if current.Status == FinanceCaseProviderConfirmed {
		mismatchJournal,postErr := store.PostJournal(ctx,"mismatch-"+orderID,"OMR","mismatched journal",
			[]JournalLine{{AccountCode:"provider_payable",Side:"debit",AmountMinor:299,Currency:"OMR"},
				{AccountCode:"customer_funds",Side:"credit",AmountMinor:299,Currency:"OMR"}})
		if postErr != nil { t.Fatal(postErr) }
		if _,err = store.ReconcileFinanceCase(ctx,approver,current.ID,mismatchJournal.ID,"Investigate amount discrepancy"); !errors.Is(err,ErrFinanceCaseConflict) {
			t.Fatalf("mismatched journal must not reconcile: %v",err)
		}
		if err = pool.QueryRow(ctx,`SELECT count(*) FROM finance_case_reconciliation_checks
			WHERE case_id=$1 AND outcome='amount_mismatch'`,current.ID).Scan(&links); err != nil { t.Fatal(err) }
		if links != 1 { t.Fatalf("expected durable amount discrepancy, got %d",links) }
	}
}
