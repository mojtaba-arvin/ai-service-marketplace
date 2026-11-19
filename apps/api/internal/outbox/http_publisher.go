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
 "strings"
 "time"
)

var ErrPublisherConfig=errors.New("invalid outbox publisher configuration")

type HTTPPublishError struct { StatusCode int }
func (e *HTTPPublishError) Error()string{return fmt.Sprintf("outbox sink returned HTTP %d",e.StatusCode)}

type HTTPPublisher struct { endpoint *url.URL; token string; client *http.Client }

func NewHTTPPublisher(endpoint,token string,client *http.Client)(*HTTPPublisher,error){
 parsed,err:=url.Parse(strings.TrimSpace(endpoint))
 if err!=nil||parsed.Scheme!="https"||parsed.Host==""||parsed.User!=nil||parsed.Fragment!=""||strings.TrimSpace(token)==""{return nil,ErrPublisherConfig}
 if client==nil{client=&http.Client{Timeout:10*time.Second}}
 safe:=*client
 safe.CheckRedirect=func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}
 return &HTTPPublisher{endpoint:parsed,token:token,client:&safe},nil
}

func (p *HTTPPublisher) Publish(ctx context.Context,event Event,idempotencyKey string)error{
 if p==nil||p.endpoint==nil||p.client==nil||p.token==""||event.ID<=0||idempotencyKey==""||!json.Valid(event.Payload){return ErrPublisherConfig}
 body,err:=json.Marshal(struct{
  ID int64 `json:"id"`
  AggregateType string `json:"aggregate_type"`
  AggregateID string `json:"aggregate_id"`
  EventType string `json:"event_type"`
  Payload json.RawMessage `json:"payload"`
 }{event.ID,event.AggregateType,event.AggregateID,event.EventType,event.Payload})
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
