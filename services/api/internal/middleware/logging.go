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

// invitationsPathPrefix is the route prefix whose next path segment is the raw
// invitation token — a bearer credential (the server stores only sha256(raw),
// and the raw grants org membership on /accept). chi resolves {token} from
// r.URL.Path, so the path literally embeds the secret. RequestLogger runs as a
// top-level middleware before the route is matched, so chi RoutePattern is empty
// here and the token must be redacted from the path string directly.
const invitationsPathPrefix = "/api/v1/invitations/"

// acceptPathSuffix terminates the POST /accept variant of an invitation path.
const acceptPathSuffix = "/accept"

// redactInvitationToken returns path with the raw invitation token segment
// replaced by "<redacted>" for the Preview (GET .../{token}) and Accept
// (POST .../{token}/accept) invitation routes; any other path is returned
// unchanged. It rewrites only the value passed to the logger — r.URL is never
// mutated, so handlers still receive the real token.
func redactInvitationToken(path string) string {
	rest, ok := strings.CutPrefix(path, invitationsPathPrefix)
	if !ok || rest == "" {
		return path
	}
	if seg, hasAccept := strings.CutSuffix(rest, acceptPathSuffix); hasAccept {
		if seg == "" || strings.Contains(seg, "/") {
			return path
		}
		return invitationsPathPrefix + "<redacted>" + acceptPathSuffix
	}
	if strings.Contains(rest, "/") {
		return path
	}
	return invitationsPathPrefix + "<redacted>"
}

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
				slog.String("path", redactInvitationToken(r.URL.Path)),
				slog.String("remote_addr", r.RemoteAddr),
			)

			wrapped := wrapResponseWriter(w)

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			logger.LogAttrs(ctx, completedLevel(wrapped.status), "request completed",
				slog.String("method", r.Method),
				slog.String("path", redactInvitationToken(r.URL.Path)),
				slog.Int("status", wrapped.status),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}
