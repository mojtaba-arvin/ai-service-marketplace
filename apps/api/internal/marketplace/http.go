package marketplace

import (
 "encoding/json"
 "errors"
 "net/http"
 "strings"
 "time"
)

type OTPDelivery interface { SendOTP(rctx interface{ Done() <-chan struct{}; Err() error; Deadline()(time.Time,bool); Value(any) any },phone,code,idempotencyKey string) error }

type Handler struct {
 Store *Store
 OTPCode string
 TestMode bool
 OTPPepper []byte
 SessionPepper []byte
 OTPDelivery OTPDelivery
 AccessTTL time.Duration
 RefreshTTL time.Duration
}

func (h *Handler) ServeHTTP(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 switch {
 case r.Method==http.MethodGet&&r.URL.Path=="/healthz":
  if err:=h.Store.Health(r.Context());err!=nil{writeError(w,http.StatusServiceUnavailable,"database unavailable");return}
  writeJSON(w,http.StatusOK,map[string]string{"status":"ok"})
 case r.Method==http.MethodPost&&r.URL.Path=="/v1/test/otp/verify":
  h.verifyTestOTP(w,r)
 case r.Method==http.MethodPost&&r.URL.Path=="/v1/auth/otp/request":
  h.requestOTP(w,r)
 case r.Method==http.MethodPost&&r.URL.Path=="/v1/auth/otp/verify":
  h.verifyOTP(w,r)
 case r.Method==http.MethodPost&&r.URL.Path=="/v1/auth/session/refresh":
  h.refreshSession(w,r)
 case r.Method==http.MethodPost&&r.URL.Path=="/v1/auth/session/revoke":
  h.revokeSession(w,r)
 case r.Method==http.MethodGet&&r.URL.Path=="/v1/auth/me":
  h.authMe(w,r)
 case r.Method==http.MethodGet&&strings.HasPrefix(r.URL.Path,"/v1/orders/"):
  h.getOrder(w,r)
 case r.Method==http.MethodPost&&r.URL.Path=="/v1/orders":
  h.createOrder(w,r)
 case r.Method==http.MethodPost&&strings.HasPrefix(r.URL.Path,"/v1/orders/")&&strings.HasSuffix(r.URL.Path,"/transition"):
  h.transitionOrder(w,r)
 default:writeError(w,http.StatusNotFound,"not found")
 }
}

func (h *Handler) verifyTestOTP(w http.ResponseWriter,r *http.Request){
 if !h.TestMode{writeError(w,http.StatusNotFound,"not found");return}
 var in struct{Phone string `json:"phone"`;Code string `json:"code"`}
 if json.NewDecoder(r.Body).Decode(&in)!=nil||in.Phone==""||in.Code==""{writeError(w,http.StatusBadRequest,"phone and code are required");return}
 if in.Code!=h.OTPCode{writeError(w,http.StatusUnauthorized,"invalid test code");return}
 writeJSON(w,http.StatusOK,map[string]string{"access_token":"test-session","customer_id":"test-customer"})
}

func (h *Handler) principal(r *http.Request)(SessionPrincipal,error){
 header:=r.Header.Get("Authorization")
 if !strings.HasPrefix(header,"Bearer ")||strings.TrimSpace(strings.TrimPrefix(header,"Bearer "))==""{return SessionPrincipal{},ErrSessionInvalid}
 token:=strings.TrimSpace(strings.TrimPrefix(header,"Bearer "))
 if h.TestMode&&token=="test-session"{return SessionPrincipal{SessionID:"test-session",IdentityID:"test-customer",Phone:"+10000000000"},nil}
 if len(h.SessionPepper)<sessionPepperMinBytes{return SessionPrincipal{},ErrSessionInvalid}
 return h.Store.AuthenticateAccessToken(r.Context(),token,h.SessionPepper)
}

func (h *Handler) requestOTP(w http.ResponseWriter,r *http.Request){
 if h.TestMode{writeError(w,http.StatusNotFound,"not found");return}
 if h.OTPDelivery==nil||len(h.OTPPepper)<otpPepperMinBytes{writeError(w,http.StatusServiceUnavailable,"OTP delivery is not configured");return}
 var in struct{Phone string `json:"phone"`}
 if json.NewDecoder(r.Body).Decode(&in)!=nil||in.Phone==""{writeError(w,http.StatusBadRequest,"phone is required");return}
 code,err:=GenerateOTPCode();if err!=nil{writeError(w,http.StatusInternalServerError,"could not create OTP");return}
 challengeID,err:=h.Store.CreateOTPChallenge(r.Context(),in.Phone,code,h.OTPPepper,5*time.Minute,5)
 if err!=nil{
  status:=http.StatusBadRequest;if errors.Is(err,ErrOTPRateLimited){status=http.StatusTooManyRequests}
  writeError(w,status,err.Error());return
 }
 if err=h.OTPDelivery.SendOTP(r.Context(),in.Phone,code,"otp:"+challengeID);err!=nil{
  _=h.Store.InvalidateOTPChallenge(r.Context(),challengeID)
  writeError(w,http.StatusBadGateway,"OTP delivery failed");return
 }
 writeJSON(w,http.StatusAccepted,map[string]any{"challenge_id":challengeID,"expires_in_seconds":300})
}

func (h *Handler) verifyOTP(w http.ResponseWriter,r *http.Request){
 if h.TestMode{writeError(w,http.StatusNotFound,"not found");return}
 var in struct{ChallengeID string `json:"challenge_id"`;Code string `json:"code"`}
 if json.NewDecoder(r.Body).Decode(&in)!=nil||in.ChallengeID==""||in.Code==""{writeError(w,http.StatusBadRequest,"challenge_id and code are required");return}
 verified,err:=h.Store.VerifyOTPChallenge(r.Context(),in.ChallengeID,in.Code,h.OTPPepper)
 if err!=nil{writeError(w,http.StatusUnauthorized,"invalid or expired OTP challenge");return}
 accessTTL,refreshTTL:=h.sessionTTLs()
 tokens,err:=h.Store.IssueSession(r.Context(),verified,h.SessionPepper,accessTTL,refreshTTL)
 if err!=nil{writeError(w,http.StatusInternalServerError,"could not issue session");return}
 writeJSON(w,http.StatusOK,tokens)
}

func (h *Handler) refreshSession(w http.ResponseWriter,r *http.Request){
 if h.TestMode{writeError(w,http.StatusNotFound,"not found");return}
 var in struct{RefreshToken string `json:"refresh_token"`}
 if json.NewDecoder(r.Body).Decode(&in)!=nil||in.RefreshToken==""{writeError(w,http.StatusBadRequest,"refresh_token is required");return}
 accessTTL,refreshTTL:=h.sessionTTLs()
 tokens,err:=h.Store.RotateSession(r.Context(),in.RefreshToken,h.SessionPepper,accessTTL,refreshTTL)
 if err!=nil{writeError(w,http.StatusUnauthorized,"refresh token is invalid or has been replayed");return}
 writeJSON(w,http.StatusOK,tokens)
}

func (h *Handler) revokeSession(w http.ResponseWriter,r *http.Request){
 principal,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
 if principal.SessionID=="test-session"{writeError(w,http.StatusNotImplemented,"test session revocation is not supported");return}
 token:=strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer "))
 if err=h.Store.RevokeSession(r.Context(),token,h.SessionPepper);err!=nil{writeError(w,http.StatusUnauthorized,"session is invalid");return}
 writeJSON(w,http.StatusOK,map[string]string{"status":"revoked"})
}

func (h *Handler) authMe(w http.ResponseWriter,r *http.Request){
 principal,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
 writeJSON(w,http.StatusOK,map[string]string{"session_id":principal.SessionID,"identity_id":principal.IdentityID,"phone":principal.Phone})
}

func (h *Handler) getOrder(w http.ResponseWriter,r *http.Request){
 principal,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
 id:=strings.TrimPrefix(r.URL.Path,"/v1/orders/")
 if id==""||strings.Contains(id,"/"){writeError(w,http.StatusNotFound,"not found");return}
 order,err:=h.Store.GetOrderForCustomer(r.Context(),id,principal.IdentityID)
 if err!=nil{writeError(w,http.StatusNotFound,"order not found");return}
 writeJSON(w,http.StatusOK,map[string]any{"order":order})
}

func (h *Handler) createOrder(w http.ResponseWriter,r *http.Request){
 if !h.TestMode{writeError(w,http.StatusNotImplemented,"order creation is disabled until production pricing and ledger policies are configured");return}
 principal,err:=h.principal(r);if err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
 var in struct{AmountMinor int64 `json:"amount_minor"`;Currency string `json:"currency"`}
 if json.NewDecoder(r.Body).Decode(&in)!=nil{writeError(w,http.StatusBadRequest,"invalid JSON");return}
 order,created,err:=h.Store.CreateOrder(r.Context(),principal.IdentityID,r.Header.Get("Idempotency-Key"),in.AmountMinor,in.Currency)
 if err!=nil{
  status:=http.StatusBadRequest;if errors.Is(err,ErrInvalidAmount)||errors.Is(err,ErrInvalidCurrency){status=http.StatusUnprocessableEntity}
  writeError(w,status,err.Error());return
 }
 code:=http.StatusOK;if created{code=http.StatusCreated}
 writeJSON(w,code,map[string]any{"order":order,"created":created,"ledger":"simulated-test-only"})
}

func (h *Handler) transitionOrder(w http.ResponseWriter,r *http.Request){
 if !h.TestMode{writeError(w,http.StatusNotImplemented,"order transitions are disabled until production actor policy is configured");return}
 if _,err:=h.principal(r);err!=nil{writeError(w,http.StatusUnauthorized,"unauthorized");return}
 parts:=strings.Split(strings.Trim(r.URL.Path,"/"),"/")
 if len(parts)!=4{writeError(w,http.StatusNotFound,"not found");return}
 var in struct{To string `json:"to"`}
 if json.NewDecoder(r.Body).Decode(&in)!=nil{writeError(w,http.StatusBadRequest,"invalid JSON");return}
 order,err:=h.Store.TransitionOrder(r.Context(),parts[2],in.To)
 if err!=nil{if errors.Is(err,ErrInvalidTransition){writeError(w,http.StatusConflict,err.Error());return};writeError(w,http.StatusNotFound,"order not found or transition failed");return}
 writeJSON(w,http.StatusOK,map[string]any{"order":order})
}
func (h *Handler) sessionTTLs()(time.Duration,time.Duration){
 access,refresh:=h.AccessTTL,h.RefreshTTL
 if access==0{access=15*time.Minute};if refresh==0{refresh=30*24*time.Hour}
 return access,refresh
}
func writeJSON(w http.ResponseWriter,status int,v any){w.WriteHeader(status);_=json.NewEncoder(w).Encode(v)}
func writeError(w http.ResponseWriter,status int,message string){writeJSON(w,status,map[string]string{"error":message})}
