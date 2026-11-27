package observability

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
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

func (w *responseRecorder) Flush() { _ = w.FlushError() }

func (w *responseRecorder) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(interface{ FlushError() error }); ok {
		return f.FlushError()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
		return nil
	}
	return http.ErrNotSupported
}

func (w *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (w *responseRecorder) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

var fallbackRequestID atomic.Uint64

func requestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		binary.BigEndian.PutUint64(raw[:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(raw[8:], fallbackRequestID.Add(1))
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
		defer func() {
			recovered := recover()
			panicked := recovered != nil
			if recorder.status == 0 {
				if panicked {
					recorder.status = http.StatusInternalServerError
				} else {
					recorder.status = http.StatusOK
				}
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
				slog.String("request_id", id),
				slog.String("method", r.Method),
				slog.String("route_class", routeClass(r.URL.Path)),
				slog.Int("status", recorder.status),
				slog.Int64("duration_ms", time.Since(started).Milliseconds()),
				slog.Int("response_bytes", recorder.bytes),
				slog.Bool("panicked", panicked),
			)
			if panicked {
				panic(recovered)
			}
		}()
		next.ServeHTTP(recorder, r)
	})
}

func routeClass(path string) string {
	switch {
	case path == "/healthz" || path == "/readyz":
		return "health"
	case strings.HasPrefix(path, "/v1/"):
		return "api"
	default:
		return "other"
	}
}
