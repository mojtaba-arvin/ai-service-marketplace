package marketplace

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
)

type Handler struct { Store *Store; OTPCode string }

func (h *Handler) ServeHTTP(w http.ResponseWriter,r *http.Request) {
	w.Header().Set("Content-Type","application/json")
	switch {
	case r.Method==http.MethodGet && r.URL.Path=="/healthz":
		if err:=h.Store.Health(r.Context()); err!=nil { writeError(w,http.StatusServiceUnavailable,"database unavailable"); return }
		writeJSON(w,http.StatusOK,map[string]string{"status":"ok"})
	case r.Method==http.MethodPost && r.URL.Path=="/v1/test/otp/verify":
		h.verifyTestOTP(w,r)
	case r.Method==http.MethodPost && r.URL.Path=="/v1/orders":
		h.createOrder(w,r)
	case r.Method==http.MethodPost && strings.HasPrefix(r.URL.Path,"/v1/orders/") && strings.HasSuffix(r.URL.Path,"/transition"):
		h.transitionOrder(w,r)
	default:
		writeError(w,http.StatusNotFound,"not found")
	}
}

func (h *Handler) testMode() bool {
	env:=os.Getenv("APP_ENV")
	return env=="" || env=="development" || env=="test"
}
func (h *Handler) verifyTestOTP(w http.ResponseWriter,r *http.Request) {
	if !h.testMode() { writeError(w,http.StatusNotFound,"not found"); return }
	var in struct { Phone string `json:"phone"`; Code string `json:"code"` }
	if json.NewDecoder(r.Body).Decode(&in)!=nil || in.Phone=="" || in.Code=="" { writeError(w,http.StatusBadRequest,"phone and code are required"); return }
	if in.Code!=h.OTPCode { writeError(w,http.StatusUnauthorized,"invalid test code"); return }
	writeJSON(w,http.StatusOK,map[string]string{"access_token":"test-session","customer_id":"test-customer"})
}
func (h *Handler) authenticated(r *http.Request) bool {
	return h.testMode() && r.Header.Get("Authorization")=="Bearer test-session"
}
func (h *Handler) createOrder(w http.ResponseWriter,r *http.Request) {
	if !h.authenticated(r) { writeError(w,http.StatusUnauthorized,"unauthorized"); return }
	var in struct { AmountMinor int64 `json:"amount_minor"`; Currency string `json:"currency"` }
	if json.NewDecoder(r.Body).Decode(&in)!=nil { writeError(w,http.StatusBadRequest,"invalid JSON"); return }
	order,created,err:=h.Store.CreateOrder(r.Context(),"test-customer",r.Header.Get("Idempotency-Key"),in.AmountMinor,in.Currency)
	if err!=nil {
		status:=http.StatusBadRequest
		if errors.Is(err,ErrInvalidAmount)||errors.Is(err,ErrInvalidCurrency) { status=http.StatusUnprocessableEntity }
		writeError(w,status,err.Error()); return
	}
	code:=http.StatusOK; if created { code=http.StatusCreated }
	writeJSON(w,code,map[string]any{"order":order,"created":created,"ledger":"simulated-test-only"})
}
func (h *Handler) transitionOrder(w http.ResponseWriter,r *http.Request) {
	if !h.authenticated(r) { writeError(w,http.StatusUnauthorized,"unauthorized"); return }
	parts:=strings.Split(strings.Trim(r.URL.Path,"/"),"/")
	if len(parts)!=4 { writeError(w,http.StatusNotFound,"not found"); return }
	var in struct { To string `json:"to"` }
	if json.NewDecoder(r.Body).Decode(&in)!=nil { writeError(w,http.StatusBadRequest,"invalid JSON"); return }
	order,err:=h.Store.TransitionOrder(r.Context(),parts[2],in.To)
	if err!=nil {
		if errors.Is(err,ErrInvalidTransition) { writeError(w,http.StatusConflict,err.Error()); return }
		writeError(w,http.StatusNotFound,"order not found or transition failed"); return
	}
	writeJSON(w,http.StatusOK,map[string]any{"order":order})
}
func writeJSON(w http.ResponseWriter,status int,v any) { w.WriteHeader(status); _=json.NewEncoder(w).Encode(v) }
func writeError(w http.ResponseWriter,status int,message string) { writeJSON(w,status,map[string]string{"error":message}) }
