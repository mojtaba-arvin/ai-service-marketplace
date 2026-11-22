package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPLogsBoundedPrivacySafeRequestTelemetry(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})
	handler := HTTP(next, logger)
	req := httptest.NewRequest(http.MethodPost, "/v1/orders/private-order-id?phone=%2B96890000000", strings.NewReader("private-body"))
	req.Header.Set("X-Request-ID", "attacker-controlled")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}
	requestID := response.Header().Get("X-Request-ID")
	if len(requestID) != 32 || requestID == "attacker-controlled" {
		t.Fatalf("server must generate a 128-bit request ID, got %q", requestID)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatalf("decode structured log: %v", err)
	}
	if record["request_id"] != requestID || record["method"] != http.MethodPost ||
		record["route_class"] != "api" || record["status"] != float64(http.StatusCreated) ||
		record["response_bytes"] != float64(len("created")) {
		t.Fatalf("unexpected request telemetry: %#v", record)
	}
	logged := logs.String()
	for _, forbidden := range []string{"private-order-id", "+96890000000", "private-body", "attacker-controlled"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("sensitive request detail %q leaked into logs: %s", forbidden, logged)
		}
	}
}

func TestHTTPDefaultsUnwrittenResponseToOK(t *testing.T) {
	var logs bytes.Buffer
	handler := HTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), slog.New(slog.NewJSONHandler(&logs, nil)))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !strings.Contains(logs.String(), `"status":200`) || !strings.Contains(logs.String(), `"route_class":"health"`) {
		t.Fatalf("implicit 200 response was not recorded: %s", logs.String())
	}
}
