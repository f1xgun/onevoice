package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// resetConfirmPathFragment matches any URL whose path contains the
// password-reset confirm endpoint. Phase 21b D-16 + PITFALLS §1.4 require
// belt-and-suspenders defense against token leakage via access logs /
// downstream middleware: even though RequestLogger today only logs
// r.URL.Path (which is query-string-free by Go stdlib semantics), a
// future refactor or a downstream middleware (Sentry, panic recovery,
// request-id chain) could surface r.URL.RawQuery. Scrubbing RawQuery here
// makes the token unreachable for any handler later in the chain.
const resetConfirmPathFragment = "/auth/password-reset/confirm"

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// RequestLogger creates a request logging middleware using structured logging
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Phase 21b D-16 + PITFALLS §1.4: defensively strip the
			// query string from the URL on the password-reset confirm
			// path. r.URL.Path is already query-string-free, so the
			// existing slog.String("path", r.URL.Path) below is safe
			// today. We mutate r.URL.RawQuery here so any DOWNSTREAM
			// middleware (panic recovery, Sentry, request-id) or a
			// future log-format change cannot surface the ?token=…
			// fragment for THIS path specifically. Path is preserved
			// so chi can still route the request.
			if strings.Contains(r.URL.Path, resetConfirmPathFragment) {
				r.URL.RawQuery = ""
			}
			start := time.Now()

			// Log request start
			logger.Info("request started",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
			)

			// Wrap response writer to capture status
			wrapped := wrapResponseWriter(w)

			// Call next handler
			next.ServeHTTP(wrapped, r)

			// Calculate duration
			duration := time.Since(start)

			// Log request completion
			logger.Info("request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.status),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}
