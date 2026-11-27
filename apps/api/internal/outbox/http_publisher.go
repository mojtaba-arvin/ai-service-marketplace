package outbox

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strconv"
 "strings"
 "time"
)

var ErrPublisherConfig=errors.New("invalid outbox publisher configuration")

type HTTPPublishError struct { StatusCode int }
func (e *HTTPPublishError) Error()string{return fmt.Sprintf("outbox sink returned HTTP %d",e.StatusCode)}

type HTTPPublisher struct { endpoint *url.URL; token string; client *http.Client }

// canonicalDomainEvent recognizes the versioned envelope without reinterpreting
// legacy business payloads. A malformed envelope-shaped object fails closed.
func canonicalDomainEvent(event Event) (*DomainEvent, error) {
 var fields map[string]json.RawMessage
 if err := json.Unmarshal(event.Payload, &fields); err != nil || fields == nil {
  return nil, nil
 }
 if _, versioned := fields["version"]; !versioned {
  return nil, nil
 }
 if _, tenantScoped := fields["tenant_id"]; !tenantScoped {
  if _, aggregate := fields["aggregate"]; !aggregate {
   if _, data := fields["data"]; !data { return nil, nil }
  }
 }
 var domain DomainEvent
 if err := json.Unmarshal(event.Payload, &domain); err != nil || ValidateDomainEvent(domain) != nil ||
  domain.ID == "" || domain.Version != 1 || domain.Type != event.EventType ||
  domain.Aggregate.Type != event.AggregateType || domain.Aggregate.ID != event.AggregateID {
  return nil, ErrPublisherConfig
 }
 return &domain, nil
}

func NewHTTPPublisher(endpoint,token string,client *http.Client)(*HTTPPublisher,error){
 parsed,err:=url.Parse(strings.TrimSpace(endpoint))
 if err!=nil||parsed.Scheme!="https"||parsed.Host==""||parsed.User!=nil||parsed.Fragment!=""||strings.TrimSpace(token)==""{return nil,ErrPublisherConfig}
 if client==nil{client=&http.Client{Timeout:10*time.Second}}
 safe:=*client
 safe.CheckRedirect=func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}
 return &HTTPPublisher{endpoint:parsed,token:token,client:&safe},nil
}

func (p *HTTPPublisher) Publish(ctx context.Context,event Event,idempotencyKey string)error{
 if p==nil||p.endpoint==nil||p.client==nil||p.token==""||event.ID<=0||idempotencyKey!=strconv.FormatInt(event.ID,10)||!json.Valid(event.Payload){return ErrPublisherConfig}
 domain,err:=canonicalDomainEvent(event)
 if err!=nil{return err}
 payload:=event.Payload
 if domain!=nil { payload=domain.Data }
 body,err:=json.Marshal(struct{
  ID int64 `json:"id"`
  AggregateType string `json:"aggregate_type"`
  AggregateID string `json:"aggregate_id"`
  EventType string `json:"event_type"`
  Payload json.RawMessage `json:"payload"`
  DomainEvent *DomainEvent `json:"domain_event,omitempty"`
 }{event.ID,event.AggregateType,event.AggregateID,event.EventType,payload,domain})
 if err!=nil{return err}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,p.endpoint.String(),bytes.NewReader(body));if err!=nil{return err}
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("Authorization","Bearer "+p.token)
 req.Header.Set("Idempotency-Key",idempotencyKey)
 resp,err:=p.client.Do(req);if err!=nil{return fmt.Errorf("publish outbox event %d: %w",event.ID,err)}
 defer resp.Body.Close()
 _,_=io.Copy(io.Discard,io.LimitReader(resp.Body,4096))
 if resp.StatusCode<200||resp.StatusCode>=300{return &HTTPPublishError{StatusCode:resp.StatusCode}}
 return nil
}

// RetryableHTTP treats network failures and transient HTTP statuses as retryable.
// Other 4xx responses are permanent and should be dead-lettered immediately.
func RetryableHTTP(err error)bool{
 var responseErr *HTTPPublishError
 if !errors.As(err,&responseErr){return true}
 switch responseErr.StatusCode{
 case http.StatusRequestTimeout,425,http.StatusTooManyRequests:
  return true
 default:
  return responseErr.StatusCode>=500
 }
}
