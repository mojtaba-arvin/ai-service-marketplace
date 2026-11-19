package marketplace

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

var ErrOTPDeliveryConfig=errors.New("invalid OTP delivery configuration")

type HTTPOTPDelivery struct {
 endpoint *url.URL
 token string
 client *http.Client
}

func NewHTTPOTPDelivery(endpoint,token string,client *http.Client)(*HTTPOTPDelivery,error){
 parsed,err:=url.Parse(strings.TrimSpace(endpoint))
 if err!=nil||parsed.Scheme!="https"||parsed.Host==""||parsed.User!=nil||parsed.Fragment!=""||strings.TrimSpace(token)==""{
  return nil,ErrOTPDeliveryConfig
 }
 if client==nil{client=&http.Client{Timeout:8*time.Second}}
 safeClient:=*client
 safeClient.CheckRedirect=func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}
 return &HTTPOTPDelivery{endpoint:parsed,token:token,client:&safeClient},nil
}

// SendOTP sends only to a configured HTTPS endpoint and reuses the challenge
// identifier as the provider idempotency key.
func (d *HTTPOTPDelivery) SendOTP(ctx context.Context,phone,code,idempotencyKey string)error{
 if d==nil||d.endpoint==nil||d.client==nil||d.token==""||phone==""||len(code)!=otpCodeLength||idempotencyKey==""{
  return ErrOTPDeliveryConfig
 }
 body,err:=json.Marshal(map[string]string{"phone":phone,"code":code})
 if err!=nil{return err}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,d.endpoint.String(),bytes.NewReader(body))
 if err!=nil{return err}
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("Authorization","Bearer "+d.token)
 req.Header.Set("Idempotency-Key",idempotencyKey)
 resp,err:=d.client.Do(req)
 if err!=nil{return fmt.Errorf("OTP delivery request failed: %w",err)}
 defer resp.Body.Close()
 _,_=io.Copy(io.Discard,io.LimitReader(resp.Body,4096))
 if resp.StatusCode<200||resp.StatusCode>=300{return fmt.Errorf("OTP delivery provider returned HTTP %d",resp.StatusCode)}
 return nil
}
