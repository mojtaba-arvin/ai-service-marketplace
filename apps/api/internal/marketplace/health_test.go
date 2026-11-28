package marketplace

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzIsProcessLivenessWithoutDatabase(t *testing.T) {
	handler := &Handler{}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("liveness returned %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestReadyzFailsClosedWithoutDatabase(t *testing.T) {
	handler := &Handler{}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness returned %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}
