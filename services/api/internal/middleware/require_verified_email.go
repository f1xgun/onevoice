// Package middleware — require_verified_email.go
//
// Phase 21-03 (ACCT-02 / D-26..D-30): soft-restrict middleware. Two
// variants over a shared helper:
//
//   - RequireVerifiedEmailDay0: hard-blocks unverified users IMMEDIATELY
//     (regardless of account age). Decorates POST /integrations/*,
//     POST /invitations, PATCH /users/me/email — attacker surfaces that
//     cannot be granted to unverified users at any time.
//
//   - RequireVerifiedEmailDay7: blocks unverified users ONLY after the
//     7-day grace window has elapsed (NOW() - created_at >= 7d).
//     Decorates POST /chat/{id}, POST /businesses — full conversion
//     access is preserved during the grace window so soft-restrict is
//     truly soft.
//
// Routes that NEVER decorate (D-30):
//   - DELETE /users/me                  — right to erasure cannot be gated
//   - /auth/verify-email/* and resend   — the verify path itself
//   - PATCH /auth/email-before-verify   — escape hatch for dead email-on-file
//   - GET /api/v1/*                     — read endpoints are banner-only
//
// Response shape on block:
//
//	HTTP 412 Precondition Failed
//	{
//	  "code": "email_verification_required",
//	  "verifiedDeadline": "<ISO8601 created_at + 7 days>"
//	}
//
// The middleware reads users.email_verified from the DB on every request
// (T-VE-03 mitigation — stale JWTs cannot bypass a flag that was flipped
// after token issue). Acceptable extra round-trip for v1.4; can grow a
// 30s LRU cache in a follow-up if perf matters.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// emailVerifyGraceDuration mirrors the handler-side constant
// (handler.emailVerifyGraceDuration). Both must agree on D-28's 7-day
// grace value; the duplication is intentional to avoid a circular
// import (middleware → handler → middleware).
const emailVerifyGraceDuration = 7 * 24 * time.Hour

// UserLookup is the slice of domain.UserRepository the middleware needs.
// Interface-typed for test stub injection.
type UserLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
}

// RequireVerifiedEmailDay0 hard-blocks unverified users regardless of
// account age. Decorates the routes from CONTEXT D-26.
func RequireVerifiedEmailDay0(users UserLookup) func(http.Handler) http.Handler {
	return requireVerifiedEmail(users, false)
}

// RequireVerifiedEmailDay7 blocks unverified users ONLY after the 7-day
// grace window has elapsed. Decorates the routes from CONTEXT D-28.
func RequireVerifiedEmailDay7(users UserLookup) func(http.Handler) http.Handler {
	return requireVerifiedEmail(users, true)
}

func requireVerifiedEmail(users UserLookup, respectGrace bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := GetUserID(r.Context())
			if err != nil || userID == uuid.Nil {
				// No auth context — the upstream Auth middleware already
				// rejected; this middleware sits BEHIND it. Defense-in-depth:
				// pass-through and let any later handler 401 as needed.
				next.ServeHTTP(w, r)
				return
			}
			u, err := users.GetByID(r.Context(), userID)
			if err != nil {
				if errors.Is(err, domain.ErrUserNotFound) {
					// User existed at JWT-issue time but was deleted since
					// (Phase 21-04). The 401 is more correct than 500 — the
					// caller's JWT no longer maps to a real account.
					writeAuthError(w, "user_not_found", http.StatusUnauthorized)
					return
				}
				slog.ErrorContext(r.Context(), "require_verified_email: user lookup failed",
					"user_id", userID, "error", err)
				writeAuthError(w, "internal_error", http.StatusInternalServerError)
				return
			}
			if u.EmailVerified {
				next.ServeHTTP(w, r)
				return
			}
			if respectGrace && time.Since(u.CreatedAt) < emailVerifyGraceDuration {
				// Day-0..Day-7 unverified user — banner shown but writes allowed.
				next.ServeHTTP(w, r)
				return
			}
			// Block. UTC stamp on the deadline so clients in different
			// timezones still compute the same countdown.
			deadline := u.CreatedAt.Add(emailVerifyGraceDuration).UTC()
			writeVerificationRequired(w, deadline)
		})
	}
}

// writeVerificationRequired emits the 412 + {code, verifiedDeadline} body
// per the soft-restrict matrix.
func writeVerificationRequired(w http.ResponseWriter, deadline time.Time) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPreconditionFailed)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":             "email_verification_required",
		"verifiedDeadline": deadline.Format(time.RFC3339),
	})
}

// writeAuthError emits a {error: code} body matching the existing
// auth-middleware response shape (writeJSONError above).
func writeAuthError(w http.ResponseWriter, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: code})
}
