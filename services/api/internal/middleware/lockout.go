package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/f1xgun/onevoice/pkg/lockout"
)

// LockoutMiddleware mounts ONLY on POST /auth/login.
//
// Flow per request:
//  1. Peek the JSON body to extract "email". The body is restored verbatim
//     for the downstream handler so json.NewDecoder().Decode() still works.
//  2. Derive (clientIP, net16) via the trusted_proxy helpers.
//  3. lockout.GetTier:
//       TierLocked  → short-circuit 423 + retry_after_seconds.
//       TierCaptcha → annotate ctx with CaptchaRequiredKey=true; downstream
//                     handler MUST verify X-Captcha-Token before delegating
//                     to userService.Login.
//       TierNormal  → pass through, no annotation.
//  4. Always annotate ctx with LoginEmailKey + LoginClientIPKey so the
//     handler doesn't have to re-parse / re-resolve them.
//
// Fail-open on Redis errors: blocking every legitimate user during a Redis
// outage is worse than letting brute-force through for the duration of the
// outage. The error is logged for ops visibility.

// bodyPeekLimit caps the bytes read for the email probe. 1 MiB matches the
// existing chi body limits and is comfortably above the largest plausible
// login request (a fat-finger 4 KiB request is the worst case).
const bodyPeekLimit = 1 << 20

type ctxKey int

const (
	// CaptchaRequiredKey signals "this request fell in TierCaptcha — the
	// handler MUST verify the X-Captcha-Token header before calling Login".
	// Bool value, true when set.
	CaptchaRequiredKey ctxKey = iota
	// LoginEmailKey holds the email extracted during the body peek so the
	// handler doesn't re-decode just to call lockout.RecordFailure on the
	// failure path.
	LoginEmailKey
	// LoginClientIPKey holds the trusted-proxy-resolved client IP for the
	// same reason. Avoids two ClientIP() calls per request.
	LoginClientIPKey
)

type loginReqProbe struct {
	Email string `json:"email"`
}

// LockoutMiddleware returns a chi-compatible middleware that gates POST
// /auth/login by the (email_hash, /16 IP) lockout counter. See package
// doc for the full flow.
func LockoutMiddleware(lock *lockout.Lockout) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, bodyPeekLimit))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			var probe loginReqProbe
			if jerr := json.Unmarshal(bodyBytes, &probe); jerr != nil || probe.Email == "" {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := ClientIP(r)
			net16 := Net16(clientIP)
			if net16 == "" {
				next.ServeHTTP(w, r)
				return
			}

			tier, err := lock.GetTier(r.Context(), probe.Email, net16)
			if err != nil {
				slog.Warn("lockout: GetTier failed; failing open",
					slog.String("error", err.Error()),
					slog.String("client_ip", clientIP),
				)
				next.ServeHTTP(w, r)
				return
			}

			if tier == lockout.TierLocked {
				ttl, _ := lock.TTL(r.Context(), probe.Email, net16)
				writeLockedResponse(w, ttl)
				return
			}

			ctx := r.Context()
			if tier == lockout.TierCaptcha {
				ctx = context.WithValue(ctx, CaptchaRequiredKey, true)
			}
			ctx = context.WithValue(ctx, LoginEmailKey, probe.Email)
			ctx = context.WithValue(ctx, LoginClientIPKey, clientIP)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeLockedResponse emits the canonical 423 Locked body documented in
// the plan must-haves: {code: "account_locked", retry_after_seconds: <n>}.
// Also sets the Retry-After header so well-behaved HTTP clients (and CDN
// edges) can back off without parsing the body.
func writeLockedResponse(w http.ResponseWriter, ttl time.Duration) {
	seconds := int(ttl.Seconds())
	if seconds < 0 {
		seconds = 0
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusLocked)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"code":                "account_locked",
		"retry_after_seconds": seconds,
	}); err != nil {
		slog.Error("lockout: failed to encode 423 body", "error", err)
	}
}

// CaptchaRequired returns true when LockoutMiddleware annotated the
// context with CaptchaRequiredKey=true. Handler-side helper so the auth
// package doesn't have to import the ctx-key constant directly.
func CaptchaRequired(ctx context.Context) bool {
	v, ok := ctx.Value(CaptchaRequiredKey).(bool)
	return ok && v
}

// LoginEmail returns the email the lockout middleware extracted, or "" if
// the middleware did not run (e.g. a non-/auth/login route).
func LoginEmail(ctx context.Context) string {
	v, _ := ctx.Value(LoginEmailKey).(string)
	return v
}

// LoginClientIP returns the trusted-proxy-resolved client IP, or "" when
// the middleware did not run.
func LoginClientIP(ctx context.Context) string {
	v, _ := ctx.Value(LoginClientIPKey).(string)
	return v
}
