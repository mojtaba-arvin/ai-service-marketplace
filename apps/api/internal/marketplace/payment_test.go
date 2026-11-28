package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFakePaymentProviderContract(t *testing.T) {
	ctx:=context.Background()
	request:=PaymentRequest{IdempotencyKey:"payment-contract-1",AmountMinor:1250,Currency:"OMR"}
	success:=NewFakePaymentProvider(FakeProviderSuccess,[]byte("fake-webhook-secret-at-least-32-bytes"))
	first,err:=success.Authorize(ctx,request)
	if err!=nil || first.Status!="succeeded" || first.ExternalReference=="" { t.Fatalf("success authorize: %#v %v",first,err) }
	second,err:=success.Authorize(ctx,request)
	if err!=nil || second.ExternalReference!=first.ExternalReference || success.Calls()!=1 {
		t.Fatalf("fake provider must deduplicate operations: %#v %#v calls=%d err=%v",first,second,success.Calls(),err)
	}
	changedRequest:=request
	changedRequest.AmountMinor++
	if _,err=success.Authorize(ctx,changedRequest);!errors.Is(err,ErrPaymentIdempotencyConflict){
		t.Fatalf("same provider idempotency key with changed amount must fail: %v",err)
	}

	unknown:=NewFakePaymentProvider(FakeProviderTimeoutAfter,[]byte("fake-webhook-secret-at-least-32-bytes"))
	if _,err=unknown.Capture(ctx,request); !errors.Is(err,ErrProviderOutcomeUnknown) { t.Fatalf("timeout-after must report unknown outcome: %v",err) }
	resolved,err:=unknown.Capture(ctx,request)
	if err!=nil || resolved.Status!="succeeded" || unknown.Calls()!=1 { t.Fatalf("same-key retry should reveal idempotent result: %#v %v",resolved,err) }

	before:=NewFakePaymentProvider(FakeProviderTimeoutBefore,nil)
	if _,err=before.Refund(ctx,request); !errors.Is(err,context.DeadlineExceeded) { t.Fatalf("timeout-before should be a known timeout: %v",err) }
	decline:=NewFakePaymentProvider(FakeProviderDecline,nil)
	result,err:=decline.Authorize(ctx,request)
	if err!=nil || result.Status!="failed" || result.FailureCode=="" { t.Fatalf("decline contract: %#v %v",result,err) }

	event:=PaymentWebhook{EventID:"evt-1",OperationKey:request.IdempotencyKey,Status:"succeeded",ExternalReference:first.ExternalReference}
	payload,_:=json.Marshal(event)
	signature:=success.SignWebhook(payload)
	verified,err:=success.VerifyWebhook(ctx,payload,signature)
	if err!=nil || verified.EventID!=event.EventID { t.Fatalf("valid webhook: %#v %v",verified,err) }
	if _,err=success.VerifyWebhook(ctx,payload,"bad-signature"); err==nil { t.Fatal("invalid webhook signature must fail") }
}

func TestPaymentOperationAndWebhookPersistencePostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	migration,err:=os.ReadFile(filepath.Join("..","..","migrations","011_payment_adapter_contract.sql"))
	if err!=nil { t.Fatal(err) }
	if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("payment migration: %v",err) }
	store:=&Store{DB:pool}
	key:=fmt.Sprintf("payment-op-%d",time.Now().UnixNano())
	op,created,err:=store.BeginPaymentOperation(ctx,"fake","authorize",key,1250,"OMR")
	if err!=nil || !created || op.Status!="pending" { t.Fatalf("begin operation: %#v created=%v err=%v",op,created,err) }
	replayed,created,err:=store.BeginPaymentOperation(ctx,"fake","authorize",key,1250,"OMR")
	if err!=nil || created || replayed.ID!=op.ID { t.Fatalf("operation replay: %#v created=%v err=%v",replayed,created,err) }
	if _,_,err=store.BeginPaymentOperation(ctx,"fake","authorize",key,1500,"OMR"); !errors.Is(err,ErrPaymentIdempotencyConflict) {
		t.Fatalf("changed operation payload must fail: %v",err)
	}
	unknown,err:=store.RecordPaymentOutcome(ctx,op.ID,"unknown","","timeout")
	if err!=nil || unknown.Status!="unknown" { t.Fatalf("record unknown outcome: %#v %v",unknown,err) }
	resolved,err:=store.ResolveUnknownPayment(ctx,op.ID,"succeeded","provider-ref-"+key,"")
	if err!=nil || resolved.Status!="succeeded" { t.Fatalf("resolve unknown outcome: %#v %v",resolved,err) }
	if _,err=store.ResolveUnknownPayment(ctx,op.ID,"failed","","late-failure"); !errors.Is(err,ErrPaymentStateConflict) {
		t.Fatalf("resolved operation cannot transition again: %v",err)
	}

	eventID:=fmt.Sprintf("evt-%d",time.Now().UnixNano())
	payload:=[]byte(fmt.Sprintf(`{"event_id":%q,"status":"succeeded"}`,eventID))
	duplicate,err:=store.RecordPaymentWebhook(ctx,"fake",eventID,payload)
	if err!=nil || duplicate { t.Fatalf("first webhook receipt: duplicate=%v err=%v",duplicate,err) }
	duplicate,err=store.RecordPaymentWebhook(ctx,"fake",eventID,payload)
	if err!=nil || !duplicate { t.Fatalf("duplicate webhook receipt: duplicate=%v err=%v",duplicate,err) }
	if _,err=store.RecordPaymentWebhook(ctx,"fake",eventID,[]byte(`{"event_id":eventID,"status":"failed"}`)); !errors.Is(err,ErrPaymentWebhookConflict) {
		t.Fatalf("same event ID with changed payload must fail: %v",err)
	}
	if _,err=pool.Exec(ctx,`UPDATE payment_operation_events SET details='tampered' WHERE operation_id=$1`,op.ID); err==nil {
		t.Fatal("payment operation audit events must be immutable")
	}
}


// TestPaymentIdempotencyWebhookAndTerminalStateRacesPostgres coordinates
// concurrent transactions and verifies only one durable outcome wins.
func TestPaymentIdempotencyWebhookAndTerminalStateRacesPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "011_payment_adapter_contract.sql"))
	if err != nil { t.Fatal(err) }
	if _, err = execMigrationScript(ctx, pool, migration); err != nil { t.Fatalf("payment migration: %v", err) }
	store := &Store{DB: pool}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Same key and same canonical request converge to one operation.
	key := "payment-identical-race-" + suffix
	start := make(chan struct{})
	type operationResult struct { op PaymentOperation; created bool; err error }
	opResults := make(chan operationResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			op, created, callErr := store.BeginPaymentOperation(ctx, "fake", "authorize", key, 2100, "OMR")
			opResults <- operationResult{op: op, created: created, err: callErr}
		}()
	}
	close(start)
	wg.Wait()
	close(opResults)
	createdCount := 0
	var operationID string
	for r := range opResults {
		if r.err != nil { t.Fatalf("identical operation race failed: %v", r.err) }
		if r.created { createdCount++ }
		if operationID == "" { operationID = r.op.ID }
		if r.op.ID != operationID { t.Fatalf("same idempotency key returned different operations: %s != %s", operationID, r.op.ID) }
	}
	if createdCount != 1 { t.Fatalf("exactly one operation should be created, got %d", createdCount) }
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM payment_operations WHERE provider_code='fake' AND idempotency_key=$1`, key).Scan(&count); err != nil { t.Fatal(err) }
	if count != 1 { t.Fatalf("identical request race persisted %d operations", count) }

	// A changed amount racing the original request must conflict regardless of
	// which transaction inserts first; it must never create a second operation.
	conflictKey := "payment-conflicting-race-" + suffix
	conflictStart := make(chan struct{})
	conflictResults := make(chan error, 2)
	var conflictWG sync.WaitGroup
	for _, amount := range []int64{3100, 4100} {
		conflictWG.Add(1)
		go func(amount int64) {
			defer conflictWG.Done()
			<-conflictStart
			_, _, callErr := store.BeginPaymentOperation(ctx, "fake", "capture", conflictKey, amount, "OMR")
			conflictResults <- callErr
		}(amount)
	}
	close(conflictStart)
	conflictWG.Wait()
	close(conflictResults)
	successes, conflicts := 0, 0
	for callErr := range conflictResults {
		if callErr == nil { successes++ } else if errors.Is(callErr, ErrPaymentIdempotencyConflict) { conflicts++ } else { t.Fatalf("unexpected operation race error: %v", callErr) }
	}
	if successes != 1 || conflicts != 1 { t.Fatalf("changed request race must have one winner and one conflict: success=%d conflicts=%d", successes, conflicts) }
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM payment_operations WHERE provider_code='fake' AND idempotency_key=$1`, conflictKey).Scan(&count); err != nil { t.Fatal(err) }
	if count != 1 { t.Fatalf("conflicting operation race persisted %d rows", count) }

	// Duplicate delivery of an identical event is acknowledged once; changed
	// bytes under the same event ID conflict even when both receipts race.
	webhookRace := func(eventID string, bodies [][]byte) (int, int) {
		t.Helper()
		gate := make(chan struct{})
		type receiptResult struct { duplicate bool; err error }
		results := make(chan receiptResult, len(bodies))
		var group sync.WaitGroup
		for _, body := range bodies {
			group.Add(1)
			go func(payload []byte) {
				defer group.Done()
				<-gate
				duplicate, receiptErr := store.RecordPaymentWebhook(ctx, "fake", eventID, payload)
				results <- receiptResult{duplicate: duplicate, err: receiptErr}
			}(body)
		}
		close(gate)
		group.Wait()
		close(results)
		ok, duplicate, conflict := 0, 0, 0
		for r := range results {
			if r.err != nil {
				if errors.Is(r.err, ErrPaymentWebhookConflict) { conflict++; continue }
				t.Fatalf("webhook receipt race: %v", r.err)
			}
			ok++
			if r.duplicate { duplicate++ }
		}
		if ok+conflict != len(bodies) { t.Fatalf("lost webhook receipt results: ok=%d conflict=%d total=%d", ok, conflict, len(bodies)) }
		return duplicate, conflict
	}
	identicalID := "evt-identical-" + suffix
	identicalBody := []byte(`{"event_id":"identical","status":"succeeded"}`)
	duplicates, conflicts := webhookRace(identicalID, [][]byte{identicalBody, identicalBody})
	if duplicates != 1 || conflicts != 0 { t.Fatalf("identical webhook race: duplicates=%d conflicts=%d", duplicates, conflicts) }
	changedID := "evt-changed-" + suffix
	duplicates, conflicts = webhookRace(changedID, [][]byte{
		[]byte(`{"event_id":"changed","status":"succeeded"}`),
		[]byte(`{"event_id":"changed","status":"failed"}`),
	})
	if duplicates != 0 || conflicts != 1 { t.Fatalf("changed webhook race: duplicates=%d conflicts=%d", duplicates, conflicts) }
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM payment_webhook_receipts WHERE provider_code='fake' AND event_id=$1`, changedID).Scan(&count); err != nil { t.Fatal(err) }
	if count != 1 { t.Fatalf("changed webhook race persisted %d receipts", count) }

	// Two contradictory terminal callbacks race against the same pending
	// operation. Exactly one state transition and audit event may commit.
	terminalKey := "payment-terminal-race-" + suffix
	pending, _, err := store.BeginPaymentOperation(ctx, "fake", "capture", terminalKey, 5100, "OMR")
	if err != nil { t.Fatal(err) }
	terminalStart := make(chan struct{})
	terminalResults := make(chan error, 2)
	var terminalWG sync.WaitGroup
	for _, outcome := range []struct{ status, reference, failure string }{
		{"succeeded", "fake-terminal-ref-" + suffix, ""},
		{"failed", "", "late_failure"},
	} {
		terminalWG.Add(1)
		go func(status, reference, failure string) {
			defer terminalWG.Done()
			<-terminalStart
			_, outcomeErr := store.RecordPaymentOutcome(ctx, pending.ID, status, reference, failure)
			terminalResults <- outcomeErr
		}(outcome.status, outcome.reference, outcome.failure)
	}
	close(terminalStart)
	terminalWG.Wait()
	close(terminalResults)
	terminalSuccesses, terminalConflicts := 0, 0
	for outcomeErr := range terminalResults {
		if outcomeErr == nil { terminalSuccesses++ } else if errors.Is(outcomeErr, ErrPaymentStateConflict) { terminalConflicts++ } else { t.Fatalf("unexpected terminal transition race error: %v", outcomeErr) }
	}
	if terminalSuccesses != 1 || terminalConflicts != 1 { t.Fatalf("terminal transition race must have one winner: success=%d conflicts=%d", terminalSuccesses, terminalConflicts) }
	var finalStatus string
	if err = pool.QueryRow(ctx, `SELECT status FROM payment_operations WHERE id=$1`, pending.ID).Scan(&finalStatus); err != nil { t.Fatal(err) }
	if finalStatus != "succeeded" && finalStatus != "failed" { t.Fatalf("operation did not reach one terminal state: %s", finalStatus) }
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM payment_operation_events WHERE operation_id=$1 AND event_type='provider_outcome'`, pending.ID).Scan(&count); err != nil { t.Fatal(err) }
	if count != 1 { t.Fatalf("contradictory terminal race wrote %d outcome audit events", count) }
	// A stale failure delivered after a committed success is a no-op conflict;
	// replaying the identical success remains safe and cannot append another event.
	staleKey := "payment-stale-webhook-" + suffix
	stale, _, err := store.BeginPaymentOperation(ctx, "fake", "capture", staleKey, 6100, "OMR")
	if err != nil { t.Fatal(err) }
	if _, err = store.RecordPaymentOutcome(ctx, stale.ID, "succeeded", "fake-stale-ref-"+suffix, ""); err != nil { t.Fatal(err) }
	if _, err = store.RecordPaymentOutcome(ctx, stale.ID, "failed", "", "late_failure"); !errors.Is(err, ErrPaymentStateConflict) {
		t.Fatalf("out-of-order failure must not regress succeeded operation: %v", err)
	}
	if _, err = store.RecordPaymentOutcome(ctx, stale.ID, "succeeded", "fake-stale-ref-"+suffix, ""); err != nil {
		t.Fatalf("identical terminal delivery must replay safely: %v", err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM payment_operations WHERE id=$1`, stale.ID).Scan(&finalStatus); err != nil { t.Fatal(err) }
	if finalStatus != "succeeded" { t.Fatalf("stale delivery regressed terminal operation to %s", finalStatus) }

}
