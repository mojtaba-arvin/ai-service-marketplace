package adapters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

var errTemporary = errors.New("temporary provider failure")

func testPolicy() Policy {
	return Policy{Timeout: time.Second, MaxAttempts: 3, InitialBackoff: time.Millisecond,
		MaxBackoff: 2*time.Millisecond, Retryable: func(err error) bool { return errors.Is(err, errTemporary) }}
}

func TestRunnerRetriesWithStableIdempotencyKey(t *testing.T) {
	r := Runner{Policy:testPolicy()}
	calls := 0
	var keys []string
	got, err := Do(r, context.Background(), "order-123", func(_ context.Context, key string) (string,error) {
		calls++; keys=append(keys,key)
		if calls < 3 { return "",errTemporary }
		return "accepted",nil
	})
	if err != nil || got != "accepted" { t.Fatalf("got %q, %v",got,err) }
	if calls != 3 { t.Fatalf("calls=%d, want 3",calls) }
	for _, key := range keys { if key != "order-123" { t.Fatalf("unstable idempotency key: %q",key) } }
}

func TestRunnerDoesNotRetryPermanentErrors(t *testing.T) {
	r:=Runner{Policy:testPolicy()}
	permanent:=errors.New("invalid credentials")
	calls:=0
	_,err:=Do(r,context.Background(),"stable-key",func(context.Context,string)(int,error){calls++;return 0,permanent})
	if !errors.Is(err,permanent) || calls!=1 { t.Fatalf("err=%v calls=%d",err,calls) }
}

func TestRunnerRequiresIdempotencyKeyAndValidPolicy(t *testing.T) {
	r:=Runner{Policy:testPolicy()}
	if _,err:=Do(r,context.Background()," ",func(context.Context,string)(int,error){return 1,nil}); !errors.Is(err,ErrMissingIdempotencyKey) {
		t.Fatalf("expected missing key, got %v",err)
	}
	r.Policy.MaxAttempts=11
	if _,err:=Do(r,context.Background(),"key",func(context.Context,string)(int,error){return 1,nil}); !errors.Is(err,ErrInvalidPolicy) {
		t.Fatalf("expected invalid policy, got %v",err)
	}
}

func TestRunnerBoundsAttemptWithTimeout(t *testing.T) {
	p:=testPolicy(); p.Timeout=10*time.Millisecond; p.MaxAttempts=1
	r:=Runner{Policy:p}
	_,err:=Do(r,context.Background(),"key",func(ctx context.Context,_ string)(int,error){<-ctx.Done();return 0,ctx.Err()})
	if !errors.Is(err,context.DeadlineExceeded) { t.Fatalf("got %v",err) }
}

func TestCircuitBreakerOpensAndAllowsSingleProbe(t *testing.T) {
	b,err:=NewCircuitBreaker(2,20*time.Millisecond); if err!=nil { t.Fatal(err) }
	now:=time.Now()
	if !b.allow(now) { t.Fatal("closed breaker rejected call") }
	b.failure(now)
	b.failure(now)
	if b.allow(now) { t.Fatal("open breaker admitted call before cooldown") }
	if !b.allow(now.Add(21*time.Millisecond)) { t.Fatal("breaker did not admit half-open probe") }
	if b.allow(now.Add(22*time.Millisecond)) { t.Fatal("breaker admitted concurrent half-open probe") }
	b.success()
	if !b.allow(now.Add(23*time.Millisecond)) { t.Fatal("successful probe did not close breaker") }
}

func TestCircuitBreakerConcurrentAdmission(t *testing.T) {
	b,err:=NewCircuitBreaker(1,time.Second); if err!=nil { t.Fatal(err) }
	now:=time.Now(); b.failure(now)
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted:=0
	for i:=0;i<32;i++ { wg.Add(1); go func(){ defer wg.Done(); if b.allow(now.Add(2*time.Second)) { mu.Lock(); admitted++; mu.Unlock() } }() }
	wg.Wait()
	if admitted!=1 { t.Fatalf("half-open admissions=%d, want 1",admitted) }
}
