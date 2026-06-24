package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// resetConfirmPathFragment matches any URL whose path contains the
// password-reset confirm endpoint. + PITFALLS §1.4 require
// belt-and-suspenders defense against token leakage via access logs /
// downstream middleware: even though RequestLogger today only logs
// r.URL.Path (which is query-string-free by Go stdlib semantics), a
// future refactor or a downstream middleware (Sentry, panic recovery,
// request-id chain) could surface r.URL.RawQuery. Scrubbing RawQuery here
// makes the token unreachable for any handler later in the chain.
const resetConfirmPathFragment = "/auth/password-reset/confirm"

// statusServerErrorMin is the lowest 5xx status. Responses at or above it log
// the access line at Error so server faults surface above the Info request
// noise; 4xx client errors stay at Info (a 401/404 is routine, not a fault).
const statusServerErrorMin = 500

// completedLevel maps a response status to the access-log level: 5xx → Error,
// everything else → Info.
func completedLevel(status int) slog.Level {
	if status >= statusServerErrorMin {
		return slog.LevelError
	}
	return slog.LevelInfo
}

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
			if strings.Contains(r.URL.Path, resetConfirmPathFragment) {
				r.URL.RawQuery = ""
			}
			start := time.Now()
			ctx := r.Context()

			logger.InfoContext(ctx, "request started",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
			)

			wrapped := wrapResponseWriter(w)

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			logger.LogAttrs(ctx, completedLevel(wrapped.status), "request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.status),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}
