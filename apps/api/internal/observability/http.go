package observability

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func requestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "generation-failed"
	}
	return hex.EncodeToString(raw[:])
}

// HTTP records bounded, privacy-safe request telemetry. It deliberately omits
// raw paths, query strings, headers, request bodies, and identity data.
func HTTP(next http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestID()
		w.Header().Set("X-Request-ID", id)
		recorder := &responseRecorder{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
			slog.String("request_id", id),
			slog.String("method", r.Method),
			slog.String("route_class", routeClass(r.URL.Path)),
			slog.Int("status", recorder.status),
			slog.Int64("duration_ms", time.Since(started).Milliseconds()),
			slog.Int("response_bytes", recorder.bytes),
		)
	})
}

func routeClass(path string) string {
	switch {
	case path == "/healthz":
		return "health"
	case strings.HasPrefix(path, "/v1/"):
		return "api"
	default:
		return "other"
	}
}
