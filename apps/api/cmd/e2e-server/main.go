package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketplace"
)

// captureOTP is deliberately available only from this E2E-only executable.
// It exercises the real OTP challenge/session code without a third-party SMS
// account. The capture endpoint is bound to loopback and is never in the API
// production image.
type captureOTP struct {
	mu    sync.RWMutex
	codes map[string]string
}

func newCaptureOTP() *captureOTP {
	return &captureOTP{codes: make(map[string]string)}
}

func (c *captureOTP) SendOTP(_ context.Context, phone, code, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.codes[phone] = code
	return nil
}

func (c *captureOTP) latest(phone string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	code, ok := c.codes[phone]
	return code, ok
}

func required(name string, minimum int) string {
	value := strings.TrimSpace(os.Getenv(name))
	if len(value) < minimum {
		log.Fatalf("%s must contain at least %d characters", name, minimum)
	}
	return value
}

func loopbackAddress(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = fallback
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		log.Fatalf("%s must bind to 127.0.0.1 or localhost", name)
	}
	return value
}

func main() {
	if os.Getenv("APP_ENV") != "e2e" || os.Getenv("E2E_ENABLE") != "1" {
		log.Fatal("the E2E server requires APP_ENV=e2e and E2E_ENABLE=1")
	}

	databaseURL := required("DATABASE_URL", 1)
	otpPepper := required("OTP_PEPPER", 32)
	sessionPepper := required("SESSION_PEPPER", 32)
	tenantID := required("DEFAULT_TENANT_ID", 1)
	marketCode := strings.ToUpper(required("DEFAULT_MARKET_CODE", 2))
	operatorPhone := required("BOOTSTRAP_OPERATOR_PHONE", 1)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("configure E2E database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("connect E2E database: %v", err)
	}

	capture := newCaptureOTP()
	handler := &marketplace.Handler{
		Store:                  &marketplace.Store{DB: pool},
		OTPPepper:              []byte(otpPepper),
		SessionPepper:          []byte(sessionPepper),
		OTPDelivery:            capture,
		AccessTTL:              4 * time.Second,
		RefreshTTL:             15 * time.Minute,
		DefaultTenantID:        tenantID,
		DefaultMarketCode:      marketCode,
		BootstrapOperatorPhone: operatorPhone,
		CookieSecure:           true,
	}

	otpMux := http.NewServeMux()
	otpMux.HandleFunc("/__e2e/otp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		code, ok := capture.latest(r.URL.Query().Get("phone"))
		if !ok {
			http.Error(w, "OTP not captured", http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]string{"code": code}); err != nil {
			log.Printf("write E2E OTP response: %v", err)
		}
	})

	apiServer := &http.Server{
		Addr:              loopbackAddress("E2E_API_ADDR", "127.0.0.1:8080"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	otpServer := &http.Server{
		Addr:              loopbackAddress("E2E_OTP_ADDR", "127.0.0.1:8081"),
		Handler:           otpMux,
		ReadHeaderTimeout: 2 * time.Second,
	}

	errs := make(chan error, 2)
	go func() { errs <- apiServer.ListenAndServe() }()
	go func() { errs <- otpServer.ListenAndServe() }()
	log.Printf("E2E API listening on %s; OTP capture bound to loopback at %s",
		apiServer.Addr, otpServer.Addr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = apiServer.Shutdown(shutdownCtx)
		_ = otpServer.Shutdown(shutdownCtx)
	case err := <-errs:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("E2E server stopped: %v", err)
		}
	}
}
