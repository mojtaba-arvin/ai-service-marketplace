package outbox

import (
 "context"
 "encoding/json"
 "time"
 "errors"
 "net/http"
 "net/http/httptest"
 "testing"
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
