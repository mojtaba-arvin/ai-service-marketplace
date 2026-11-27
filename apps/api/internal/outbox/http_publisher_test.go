package outbox

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
)

func TestHTTPPublisherRequiresHTTPS(t *testing.T){
 if _,err:=NewHTTPPublisher("http://example.test/events","token",nil);!errors.Is(err,ErrPublisherConfig){t.Fatalf("got %v",err)}
}
func TestHTTPPublisherSendsStableIdempotencyKey(t *testing.T){
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer publisher-secret"{t.Error("missing bearer token")}
  if r.Header.Get("Idempotency-Key")!="731"{t.Errorf("wrong idempotency key: %q",r.Header.Get("Idempotency-Key"))}
  w.WriteHeader(http.StatusAccepted)
 }))
 defer server.Close()
 publisher,err:=NewHTTPPublisher(server.URL,"publisher-secret",server.Client());if err!=nil{t.Fatal(err)}
 err=publisher.Publish(context.Background(),Event{ID:731,AggregateType:"order",AggregateID:"order-1",EventType:"order.created",Payload:[]byte(`{"order_id":"order-1"}`)},"731")
 if err!=nil{t.Fatal(err)}
}
func TestHTTPPublisherClassifiesTransientAndPermanentStatuses(t *testing.T){
 for _,tc:=range []struct{status int;retryable bool}{{408,true},{425,true},{429,true},{500,true},{503,true},{400,false},{401,false},{404,false}}{
  err:=&HTTPPublishError{StatusCode:tc.status}
  if got:=RetryableHTTP(err);got!=tc.retryable{t.Errorf("status %d: got retryable=%v",tc.status,got)}
 }
 if !RetryableHTTP(errors.New("network timeout")){t.Fatal("network errors should be retried")}
}

func TestHTTPPublisherKeepsCanonicalEnvelopeAdditive(t *testing.T) {
 var received map[string]json.RawMessage
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if err:=json.NewDecoder(r.Body).Decode(&received);err!=nil { t.Error(err) }
  w.WriteHeader(http.StatusAccepted)
 }))
 defer server.Close()
 publisher,err:=NewHTTPPublisher(server.URL,"publisher-secret",server.Client());if err!=nil{t.Fatal(err)}
 data:=json.RawMessage(`{"order_id":"order-1","status":"requested"}`)
 domain:=DomainEvent{ID:"evt-1",Type:"order.created",Version:1,OccurredAt:time.Date(2026,11,22,12,0,0,0,time.UTC),TenantID:"tenant-1",Aggregate:EventAggregate{Type:"order",ID:"order-1"},Data:data}
 encoded,err:=json.Marshal(domain);if err!=nil{t.Fatal(err)}
 if err=publisher.Publish(context.Background(),Event{ID:731,AggregateType:"order",AggregateID:"order-1",EventType:"order.created",Payload:encoded},"731");err!=nil{t.Fatal(err)}
 var payload map[string]any
 if err=json.Unmarshal(received["payload"],&payload);err!=nil{t.Fatal(err)}
 if payload["order_id"]!="order-1" {t.Fatalf("legacy payload shape not preserved: %s",received["payload"])}
 var got DomainEvent
 if err=json.Unmarshal(received["domain_event"],&got);err!=nil{t.Fatalf("canonical envelope missing: %v",err)}
 if got.ID!="evt-1"||got.TenantID!="tenant-1"||got.Aggregate.ID!="order-1" {t.Fatalf("wrong canonical envelope: %#v",got)}
}

func TestCanonicalDomainEventRejectsMalformedEnvelopes(t *testing.T) {
 valid := DomainEvent{
  ID:"evt-1", Type:"order.created", Version:1,
  OccurredAt:time.Date(2026,11,22,12,0,0,0,time.UTC), TenantID:"tenant-1",
  Aggregate:EventAggregate{Type:"order",ID:"order-1"},
  Data:json.RawMessage(`{"order_id":"order-1","status":"requested"}`),
 }
 encoded,err:=json.Marshal(valid)
 if err!=nil { t.Fatal(err) }
 var fields map[string]json.RawMessage
 if err=json.Unmarshal(encoded,&fields);err!=nil { t.Fatal(err) }
 delete(fields,"version")
 missingVersion,err:=json.Marshal(fields)
 if err!=nil { t.Fatal(err) }
 wrongAggregate:=valid
 wrongAggregate.Aggregate.ID="order-2"
 wrongAggregatePayload,err:=json.Marshal(wrongAggregate)
 if err!=nil { t.Fatal(err) }
unsupportedVersion:=valid
 unsupportedVersion.Version=2
 unsupportedPayload,err:=json.Marshal(unsupportedVersion)
 if err!=nil { t.Fatal(err) }
 wrongAggregateType:=valid
 wrongAggregateType.Aggregate.Type="booking"
 wrongAggregateTypePayload,err:=json.Marshal(wrongAggregateType)
 if err!=nil { t.Fatal(err) }
 for name,payload:=range map[string]json.RawMessage{
  "missing version":missingVersion,
  "aggregate mismatch":wrongAggregatePayload,
  "aggregate type mismatch":wrongAggregateTypePayload,
  "unsupported schema version":unsupportedPayload,
  "missing data":json.RawMessage(`{"id":"evt-1","type":"order.created","version":1,"occurred_at":"2026-11-22T12:00:00Z","tenant_id":"tenant-1","aggregate":{"type":"order","id":"order-1"}}`),
 } {
  t.Run(name,func(t *testing.T){
   _,err:=canonicalDomainEvent(Event{ID:731,AggregateType:"order",AggregateID:"order-1",EventType:"order.created",Payload:payload})
   if !errors.Is(err,ErrPublisherConfig) { t.Fatalf("malformed canonical envelope must fail closed, got %v",err) }
  })
 }
 // A legacy payload with an unrelated version field remains legacy.
 legacy,err:=canonicalDomainEvent(Event{ID:732,AggregateType:"order",AggregateID:"order-legacy",EventType:"order.created",Payload:json.RawMessage(`{"order_id":"order-legacy","version":7,"data":{"legacy_marker":true}}`)})
 if err!=nil || legacy!=nil { t.Fatalf("legacy version field was reinterpreted: domain=%#v err=%v",legacy,err) }
}

func TestHTTPPublisherPreservesLegacyPayload(t *testing.T) {
 var received map[string]json.RawMessage
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if err:=json.NewDecoder(r.Body).Decode(&received);err!=nil { t.Error(err) }
  w.WriteHeader(http.StatusAccepted)
 }))
 defer server.Close()
 publisher,err:=NewHTTPPublisher(server.URL,"publisher-secret",server.Client());if err!=nil{t.Fatal(err)}
 legacy:=json.RawMessage(`{"order_id":"order-legacy","status":"requested","version":7}`)
 if err=publisher.Publish(context.Background(),Event{ID:732,AggregateType:"order",AggregateID:"order-legacy",EventType:"order.created",Payload:legacy},"732");err!=nil{t.Fatal(err)}
 if string(received["payload"])!=string(legacy) {t.Fatalf("legacy payload changed: got %s want %s",received["payload"],legacy)}
 if _,exists:=received["domain_event"];exists {t.Fatalf("legacy event must not gain domain_event: %s",received["domain_event"])}
}
