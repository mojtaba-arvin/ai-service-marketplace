package marketplace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHealthEndpointReturnsServiceUnavailableWhenPostgresIsDown(t *testing.T) {
	config, err := pgxpool.ParseConfig("postgres://marketplace:marketplace@127.0.0.1:1/marketplace?connect_timeout=1")
	if err != nil {
		t.Fatalf("parse unavailable PostgreSQL URL: %v", err)
	}
	config.MaxConns = 1
	config.MinConns = 0

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("create lazy PostgreSQL pool: %v", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(ctx)
	response := httptest.NewRecorder()

	handler := &Handler{Store: &Store{DB: pool}}
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("health must fail closed while PostgreSQL is unavailable: status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatalf("health response must identify database unavailability without exposing connection details: %s", response.Body.String())
	}
}
