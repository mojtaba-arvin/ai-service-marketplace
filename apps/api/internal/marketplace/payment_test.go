package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
