package marketplace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOTPRequestFailsClosedWhenDeliveryAdapterIsUnavailable(t *testing.T) {
	handler := &Handler{OTPPepper: []byte("auth-test-pepper-material-at-least-32-bytes")}
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/otp/request", strings.NewReader(`{"phone":"+15555550123"}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("OTP request must fail closed without a delivery adapter: status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "OTP delivery is not configured") {
		t.Fatalf("OTP failure must be explicit without exposing provider details: %s", response.Body.String())
	}
}
