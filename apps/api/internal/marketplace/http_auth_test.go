package marketplace

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

type fakeOTPDelivery struct{ code,key string; err error }
func (f *fakeOTPDelivery) SendOTP(_ context.Context,_ string,code,key string)error{f.code=code;f.key=key;return f.err}

func TestProductionHTTPOTPAndSessionLifecyclePostgres(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{
  "001_walking_skeleton.sql","002_identity.sql","003_sessions.sql","004_catalog.sql","005_pricing.sql",
  "006_provider_eligibility.sql","007_market_config.sql","008_catalog_localization.sql","009_ledger_accounts.sql",
  "010_ledger_idempotency_reversal.sql","011_payment_adapter_contract.sql","012_settlement_reconciliation.sql",
  "013_inspection_quotes.sql","014_quote_variations.sql","015_order_schedule.sql","016_cancellation_policy.sql",
  "017_provider_availability.sql","018_dispatch_offers.sql","019_outbox_delivery.sql","020_professional_verification.sql",
  "021_support_cases.sql","022_identity_memberships.sql","023_order_tenant_scope.sql","025_auth_devices.sql",
 }{
  raw,e:=os.ReadFile(filepath.Join("..","..","migrations",name));if e!=nil{t.Fatal(e)}
  if _,e=execMigrationScript(ctx, pool, raw);e!=nil{t.Fatalf("apply %s: %v",name,e)}
 }
 delivery:=&fakeOTPDelivery{}
 pepper:=[]byte("auth-test-pepper-material-at-least-32-bytes")
 sessionPepper:=[]byte("session-test-pepper-material-at-least-32-bytes")
 h:=&Handler{Store:&Store{DB:pool},OTPPepper:pepper,SessionPepper:sessionPepper,OTPDelivery:delivery,AccessTTL:15*time.Minute,RefreshTTL:24*time.Hour,DefaultTenantID:"auth-test-tenant",DefaultMarketCode:"OM"}
 phone:=fmt.Sprintf("+1996%011d",time.Now().UnixNano()%100000000000)
 body,_:=json.Marshal(map[string]string{"phone":phone})
 req:=httptest.NewRequest(http.MethodPost,"/v1/auth/otp/request",bytes.NewReader(body))
 response:=httptest.NewRecorder();h.ServeHTTP(response,req)
 if response.Code!=http.StatusAccepted{t.Fatalf("request OTP status=%d body=%s",response.Code,response.Body.String())}
 var challenge struct{ID string `json:"challenge_id"`}
 if err=json.Unmarshal(response.Body.Bytes(),&challenge);err!=nil{t.Fatal(err)}
 if challenge.ID==""||delivery.code==""||delivery.key!="otp:"+challenge.ID{t.Fatalf("delivery not wired to challenge: %+v %+v",challenge,delivery)}
 verifyBody,_:=json.Marshal(map[string]string{"challenge_id":challenge.ID,"code":delivery.code})
 verifyReq:=httptest.NewRequest(http.MethodPost,"/v1/auth/otp/verify",bytes.NewReader(verifyBody))
 verifyResp:=httptest.NewRecorder();h.ServeHTTP(verifyResp,verifyReq)
 if verifyResp.Code!=http.StatusOK{t.Fatalf("verify status=%d body=%s",verifyResp.Code,verifyResp.Body.String())}
 var tokens SessionTokens
 if err=json.Unmarshal(verifyResp.Body.Bytes(),&tokens);err!=nil{t.Fatal(err)}
 if tokens.AccessToken==""||tokens.RefreshToken==""{t.Fatal("session tokens missing")}
 authReq:=httptest.NewRequest(http.MethodGet,"/v1/auth/me",nil);authReq.Header.Set("Authorization","Bearer "+tokens.AccessToken)
 authResp:=httptest.NewRecorder();h.ServeHTTP(authResp,authReq)
 if authResp.Code!=http.StatusOK{t.Fatalf("auth/me status=%d body=%s",authResp.Code,authResp.Body.String())}
 var me struct {
  Phone string `json:"phone"`
  IdentityID string `json:"identity_id"`
  Roles []Role `json:"roles"`
 }
 if err=json.Unmarshal(authResp.Body.Bytes(),&me);err!=nil{t.Fatal(err)}
 if me.Phone!=phone||me.IdentityID==""{t.Fatalf("wrong principal: %+v",me)}
 order,_,err:=h.Store.CreateOrder(ctx,me.IdentityID,"auth-order-"+challenge.ID,100,"OMR");if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`UPDATE orders SET tenant_id=$2 WHERE id=$1`,order.ID,h.DefaultTenantID);err!=nil{t.Fatal(err)}
 orderReq:=httptest.NewRequest(http.MethodGet,"/v1/orders/"+order.ID,nil);orderReq.Header.Set("Authorization","Bearer "+tokens.AccessToken)
 orderResp:=httptest.NewRecorder();h.ServeHTTP(orderResp,orderReq)
 if orderResp.Code!=http.StatusOK{t.Fatalf("owner order read status=%d body=%s",orderResp.Code,orderResp.Body.String())}
 refreshBody,_:=json.Marshal(map[string]string{"refresh_token":tokens.RefreshToken})
 refreshReq:=httptest.NewRequest(http.MethodPost,"/v1/auth/session/refresh",bytes.NewReader(refreshBody))
 refreshResp:=httptest.NewRecorder();h.ServeHTTP(refreshResp,refreshReq)
 if refreshResp.Code!=http.StatusOK{t.Fatalf("refresh status=%d body=%s",refreshResp.Code,refreshResp.Body.String())}
 var rotated SessionTokens
 if err=json.Unmarshal(refreshResp.Body.Bytes(),&rotated);err!=nil{t.Fatal(err)}
 oldReq:=httptest.NewRequest(http.MethodGet,"/v1/auth/me",nil);oldReq.Header.Set("Authorization","Bearer "+tokens.AccessToken)
 oldResp:=httptest.NewRecorder();h.ServeHTTP(oldResp,oldReq)
 if oldResp.Code!=http.StatusUnauthorized{t.Fatalf("old access token survived rotation: %d",oldResp.Code)}
 revokeReq:=httptest.NewRequest(http.MethodPost,"/v1/auth/session/revoke",nil);revokeReq.Header.Set("Authorization","Bearer "+rotated.AccessToken)
 revokeResp:=httptest.NewRecorder();h.ServeHTTP(revokeResp,revokeReq)
 if revokeResp.Code!=http.StatusOK{t.Fatalf("revoke status=%d body=%s",revokeResp.Code,revokeResp.Body.String())}
 finalReq:=httptest.NewRequest(http.MethodGet,"/v1/auth/me",nil);finalReq.Header.Set("Authorization","Bearer "+rotated.AccessToken)
 finalResp:=httptest.NewRecorder();h.ServeHTTP(finalResp,finalReq)
 if finalResp.Code!=http.StatusUnauthorized{t.Fatalf("revoked access token survived: %d",finalResp.Code)}
}

func TestOTPDeliveryFailureInvalidatesChallengePostgres(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{"002_identity.sql","022_identity_memberships.sql"}{raw,e:=os.ReadFile(filepath.Join("..","..","migrations",name));if e!=nil{t.Fatal(e)};if _,e=execMigrationScript(ctx, pool, raw);e!=nil{t.Fatal(e)}}
 delivery:=&fakeOTPDelivery{err:errors.New("provider unavailable")}
 h:=&Handler{Store:&Store{DB:pool},OTPPepper:[]byte("auth-test-pepper-material-at-least-32-bytes"),OTPDelivery:delivery,DefaultTenantID:"auth-failure-tenant"}
 phone:=fmt.Sprintf("+1995%011d",time.Now().UnixNano()%100000000000)
 body,_:=json.Marshal(map[string]string{"phone":phone})
 req:=httptest.NewRequest(http.MethodPost,"/v1/auth/otp/request",bytes.NewReader(body))
 resp:=httptest.NewRecorder();h.ServeHTTP(resp,req)
 if resp.Code!=http.StatusBadGateway{t.Fatalf("got %d: %s",resp.Code,resp.Body.String())}
 challengeID:=strings.TrimPrefix(delivery.key,"otp:")
 if challengeID==""{t.Fatal("delivery did not receive idempotency key")}
 if _,err=h.Store.VerifyOTPChallenge(ctx,challengeID,delivery.code,h.OTPPepper);!errors.Is(err,ErrOTPInvalidChallenge){t.Fatalf("challenge remained usable after delivery failure: %v",err)}
}

func TestHTTPOTPDeliveryUsesHTTPSAndStableIdempotencyKey(t *testing.T){
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer secret-token"||r.Header.Get("Idempotency-Key")!="otp:challenge-1"{t.Errorf("missing delivery headers")}
  var payload map[string]string
  if err:=json.NewDecoder(r.Body).Decode(&payload);err!=nil{t.Error(err)}
  if payload["phone"]!="+96890000000"||payload["code"]!="123456"{t.Errorf("unexpected payload: %+v",payload)}
  w.WriteHeader(http.StatusAccepted)
 }))
 defer server.Close()
 delivery,err:=NewHTTPOTPDelivery(server.URL,"secret-token",server.Client());if err!=nil{t.Fatal(err)}
 if err=delivery.SendOTP(context.Background(),"+96890000000","123456","otp:challenge-1");err!=nil{t.Fatal(err)}
 if _,err=NewHTTPOTPDelivery("http://example.test","secret-token",nil);!errors.Is(err,ErrOTPDeliveryConfig){t.Fatalf("insecure endpoint accepted: %v",err)}
}

func TestHTTPDeviceManagementIsIdentityScopedAndRevokesSessionsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	for _, name := range []string{"002_identity.sql", "003_sessions.sql", "025_auth_devices.sql"} {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil { t.Fatal(readErr) }
		if _, err = execMigrationScript(ctx, pool, raw); err != nil { t.Fatalf("apply %s: %v", name, err) }
	}
	pepper := []byte("http-device-test-session-pepper-at-least-32-bytes")
	phone := fmt.Sprintf("+1993%011d", time.Now().UnixNano()%100000000000)
	if _, err = pool.Exec(ctx, `INSERT INTO phone_identities(id,phone_e164) VALUES('http-device-identity',$1)`, phone); err != nil { t.Fatal(err) }
	token := "http-device-access-token"
	if _, err = pool.Exec(ctx, `
		INSERT INTO auth_sessions(id,family_id,phone_identity_id,access_token_hash,refresh_token_hash,access_expires_at,refresh_expires_at)
		VALUES('http-device-session','http-device-family','http-device-identity',$1,$2,now()+interval '15 minutes',now()+interval '1 day')
	`, sessionDigest(token, pepper), sessionDigest("http-device-refresh", pepper)); err != nil { t.Fatal(err) }
	h := &Handler{Store:&Store{DB:pool},SessionPepper:pepper}

	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" { req.Header.Set("Content-Type", "application/json") }
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, req)
		return recorder
	}
	created := request(http.MethodPost, "/v1/auth/devices", `{"label":"Workstation"}`)
	if created.Code != http.StatusCreated { t.Fatalf("register device: status=%d body=%s", created.Code, created.Body.String()) }
	var createdBody struct { Device AuthDevice `json:"device"` }
	if err = json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil { t.Fatal(err) }
	if createdBody.Device.ID == "" { t.Fatal("device id missing") }

	listed := request(http.MethodGet, "/v1/auth/devices", "")
	if listed.Code != http.StatusOK { t.Fatalf("list devices: status=%d body=%s", listed.Code, listed.Body.String()) }
	var listBody struct { Devices []AuthDevice `json:"devices"` }
	if err = json.Unmarshal(listed.Body.Bytes(), &listBody); err != nil { t.Fatal(err) }
	if len(listBody.Devices) != 1 || listBody.Devices[0].ID != createdBody.Device.ID { t.Fatalf("unexpected device list: %#v", listBody.Devices) }

	revoked := request(http.MethodDelete, "/v1/auth/devices/"+createdBody.Device.ID, "")
	if revoked.Code != http.StatusNoContent { t.Fatalf("revoke device: status=%d body=%s", revoked.Code, revoked.Body.String()) }
	me := request(http.MethodGet, "/v1/auth/me", "")
	if me.Code != http.StatusUnauthorized { t.Fatalf("device-revoked session still authenticated: %d", me.Code) }
}
