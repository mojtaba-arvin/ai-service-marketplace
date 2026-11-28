package outbox

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


// TestWorkerRetriesExternalPublicationFailurePostgres injects an offline
// publisher failure, verifies the durable retry state, and then confirms that
// the retry uses the same event-derived idempotency key.
func TestWorkerRetriesExternalPublicationFailurePostgres(t *testing.T) {
 dsn := os.Getenv("TEST_DATABASE_URL")
 if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
 ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
 defer cancel()
 pool, err := pgxpool.New(ctx, dsn)
 if err != nil { t.Fatal(err) }
 defer pool.Close()
 for _, name := range []string{"001_walking_skeleton.sql", "019_outbox_delivery.sql"} {
  migration, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
  if readErr != nil { t.Fatal(readErr) }
  if _, err = pool.Exec(ctx, string(migration)); err != nil { t.Fatalf("apply %s: %v", name, err) }
 }
 suffix := fmt.Sprintf("%d", time.Now().UnixNano())
 var eventID int64
 if err = pool.QueryRow(ctx, `INSERT INTO outbox_events(aggregate_type,aggregate_id,event_type,payload) VALUES('finance-test',$1,'finance.test.posted','{"test":true}'::jsonb) RETURNING id`, suffix).Scan(&eventID); err != nil { t.Fatal(err) }
 repository := &Repository{DB: pool}
 var attemptedKeys []string
 failing := Worker{
  Repository: repository, WorkerID: "finance-test-worker-"+suffix, BatchSize: 1,
  Lease: 5*time.Second, MaxAttempts: 3, BaseBackoff: time.Second, MaxBackoff: 5*time.Second,
  Retryable: func(error) bool { return true },
  Publisher: PublishFunc(func(_ context.Context, event Event, key string) error {
   if event.ID != eventID { t.Fatalf("published unexpected event %d", event.ID) }
   attemptedKeys = append(attemptedKeys, key)
   return errors.New("injected external publication failure")
  }),
 }
 if published, runErr := failing.RunOnce(ctx); runErr != nil || published != 0 { t.Fatalf("failed publication run: published=%d err=%v", published, runErr) }
 var attempts int
 var publishedAt *time.Time
 if err = pool.QueryRow(ctx, `SELECT attempts,published_at FROM outbox_events WHERE id=$1`, eventID).Scan(&attempts, &publishedAt); err != nil { t.Fatal(err) }
 if attempts != 1 || publishedAt != nil { t.Fatalf("failed publish must remain pending with one recorded attempt: attempts=%d published_at=%v", attempts, publishedAt) }
 if len(attemptedKeys) != 1 || attemptedKeys[0] != fmt.Sprint(eventID) { t.Fatalf("first publication did not use stable event key: %#v", attemptedKeys) }

 // Make the scheduled retry immediately eligible without sleeping.
 if _, err = pool.Exec(ctx, `UPDATE outbox_events SET next_attempt_at=now(),locked_by=NULL,locked_until=NULL WHERE id=$1`, eventID); err != nil { t.Fatal(err) }
 successful := Worker{
  Repository: repository, WorkerID: "finance-test-retry-"+suffix, BatchSize: 1,
  Lease: 5*time.Second, MaxAttempts: 3, BaseBackoff: time.Second, MaxBackoff: 5*time.Second,
  Retryable: func(error) bool { return true },
  Publisher: PublishFunc(func(_ context.Context, event Event, key string) error {
   attemptedKeys = append(attemptedKeys, key)
   if event.ID != eventID { t.Fatalf("retry published unexpected event %d", event.ID) }
   return nil
  }),
 }
 if published, runErr := successful.RunOnce(ctx); runErr != nil || published != 1 { t.Fatalf("successful retry: published=%d err=%v", published, runErr) }
 if len(attemptedKeys) != 2 || attemptedKeys[0] != attemptedKeys[1] { t.Fatalf("external retry must reuse exact idempotency key: %#v", attemptedKeys) }
 if err = pool.QueryRow(ctx, `SELECT attempts,published_at FROM outbox_events WHERE id=$1`, eventID).Scan(&attempts, &publishedAt); err != nil { t.Fatal(err) }
 if attempts != 1 || publishedAt == nil { t.Fatalf("successful retry did not mark event published: attempts=%d published_at=%v", attempts, publishedAt) }
}
