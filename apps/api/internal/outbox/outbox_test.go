package outbox

import (
 "context"
 "errors"
 "testing"
 "time"
)

func TestRetryDelayIsCappedAndMonotonic(t *testing.T) {
 base,max:=time.Second,5*time.Second
 want:=[]time.Duration{time.Second,2*time.Second,4*time.Second,5*time.Second,5*time.Second}
 for i,expected:=range want { if got:=RetryDelay(i+1,base,max);got!=expected { t.Fatalf("attempt %d: got %v want %v",i+1,got,expected) } }
 if RetryDelay(1,0,max)!=0||RetryDelay(0,base,max)!=0 { t.Fatal("invalid retry delay should be zero") }
}
func TestRetryDelayHandlesLargeAttemptWithoutOverflow(t *testing.T) {
 if got:=RetryDelay(1000,time.Second,30*time.Second);got!=30*time.Second { t.Fatalf("got %v",got) }
}
func TestWorkerRejectsInvalidPolicyBeforeDatabaseAccess(t *testing.T) {
 w:=Worker{WorkerID:"worker",BatchSize:1,Lease:time.Second,MaxAttempts:3,BaseBackoff:time.Second,MaxBackoff:time.Second,Publisher:PublishFunc(func(_ context.Context,_ Event,_ string)error{return nil})}
 if _,err:=w.RunOnce(context.Background());!errors.Is(err,ErrInvalidPolicy) { t.Fatalf("got %v",err) }
}
func TestPermanentErrorCanBeClassifiedAsNonRetryable(t *testing.T) {
 permanent:=errors.New("permanent")
 retryable:=func(err error)bool{return !errors.Is(err,permanent)}
 if retryable(permanent) { t.Fatal("permanent error should be dead-lettered") }
}

func TestValidateCanonicalDomainEvent(t *testing.T) {
 event:=DomainEvent{
  ID:"evt-01",Type:"order.created",Version:1,OccurredAt:time.Date(2026,11,22,10,0,0,0,time.UTC),
  TenantID:"tenant-a",Aggregate:EventAggregate{Type:"order",ID:"order-01"},
  Data:[]byte(`{"status":"requested","market_code":"OM"}`),
 }
 if err:=ValidateDomainEvent(event);err!=nil { t.Fatalf("valid event rejected: %v",err) }
}

func TestCanonicalDomainEventFailsClosedForMissingTenantAndSensitiveFields(t *testing.T) {
 base:=DomainEvent{
  ID:"evt-01",Type:"order.created",Version:1,OccurredAt:time.Date(2026,11,22,10,0,0,0,time.UTC),
  TenantID:"tenant-a",Aggregate:EventAggregate{Type:"order",ID:"order-01"},
  Data:[]byte(`{"status":"requested"}`),
 }
 cases:=[]struct{name string; mutate func(*DomainEvent)}{
  {"missing tenant",func(e *DomainEvent){e.TenantID=""}},
  {"invalid event type",func(e *DomainEvent){e.Type="Order Created"}},
  {"non-object data",func(e *DomainEvent){e.Data=[]byte(`["bad"]`)}},
  {"raw phone field",func(e *DomainEvent){e.Data=[]byte(`{"customer":{"phone":"+96890000000"}}`)}},
  {"camel case phone field",func(e *DomainEvent){e.Data=[]byte(`{"customer":{"phoneNumber":"+96890000000"}}`)}},
  {"compound address field",func(e *DomainEvent){e.Data=[]byte(`{"customer_fullAddress":"sensitive"}`)}},
  {"camel case evidence reference",func(e *DomainEvent){e.Data=[]byte(`{"evidenceRef":"private://document/12345678"}`)}},
  {"nested refresh token",func(e *DomainEvent){e.Data=[]byte(`{"context":[{"refreshToken":"secret"}]}`)}},
  {"secret metadata",func(e *DomainEvent){e.Metadata=map[string]string{"access_token":"secret"}}},
  {"camel case metadata",func(e *DomainEvent){e.Metadata=map[string]string{"privateEvidenceRef":"private://document/12345678"}}},
 }
 for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){event:=base;tc.mutate(&event);if err:=ValidateDomainEvent(event);!errors.Is(err,ErrInvalidPolicy){t.Fatalf("expected fail-closed validation, got %v",err)}})}
}
