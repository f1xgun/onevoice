// Package middleware — block_writes_during_grace.go
//
// Phase 21-04 (ACCT-03 / D-34): write-gate middleware that returns 423
// Locked on POST/PUT/PATCH/DELETE during the 30-day deletion grace
// window. Reads (GET/HEAD/OPTIONS) bypass the gate so the user can
// still browse history and click restore.
//
// Routes that MUST remain reachable during grace are wired OUTSIDE this
// middleware in router.Setup — see <grace_middleware_exclusion_list>
// in the plan. The middleware itself enforces by HTTP method only
// (Option A in plan Task 8); the router decides which subtree to wrap.
//
// Reads users.deletion_requested_at via a tiny per-request SQL hit so
// stale JWTs cannot bypass the gate (T-DEL-11). DB failure → fail-OPEN
// (let request through) per plan note — we'd rather risk a write than
// 423 every request on a DB hiccup.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// graceQueryRunner is the minimal database surface BlockWritesDuringGrace
// needs. Both *pgxpool.Pool and any test double can satisfy it.
type graceQueryRunner interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// BlockWritesDuringGrace returns 423 Locked when the authenticated user
// has a pending account deletion. Must sit AFTER the Auth middleware
// so middleware.GetUserID resolves.
//
// graceDays is the constant used to compute the 423 body's
// `deletionDate` field — production callers pass 30; integration tests
// can pass a smaller value.
func BlockWritesDuringGrace(pool graceQueryRunner, graceDays int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read-method bypass (plan Task 8 / Option A). GETs are
			// the entire point of soft-restrict — user must still see
			// their /auth/me + history.
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			userID, err := GetUserID(r.Context())
			if err != nil {
				// Auth middleware should have caught; defensive pass-
				// through so the next layer can write its own 401.
				next.ServeHTTP(w, r)
				return
			}

			info, err := lookupPendingDeletion(r.Context(), pool, userID)
			if err != nil {
				// Fail-open on DB hiccup (T-DEL-11 disposition). We'd
				// rather accept a write than 423 every authenticated
				// request on a DB blip.
				slog.WarnContext(r.Context(), "block writes during grace: lookup failed; fail-open",
					"userID", userID, "err", err)
				next.ServeHTTP(w, r)
				return
			}
			if info == nil {
				next.ServeHTTP(w, r)
				return
			}

			deletionDate := info.deletionRequestedAt.Add(time.Duration(graceDays) * 24 * time.Hour)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusLocked)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":         "account_pending_deletion",
				"deletionDate": deletionDate.UTC().Format(time.RFC3339),
				"restoreUrl":   "/settings/account",
			})
		})
	}
}

type pendingDeletionInfo struct {
	deletionRequestedAt time.Time
}

// lookupPendingDeletion returns (info, nil) when the user is inside
// the grace window, (nil, nil) when no pending deletion exists, and
// (nil, err) on DB failure. Callers fail-open on err per T-DEL-11.
func lookupPendingDeletion(ctx context.Context, pool graceQueryRunner, userID uuid.UUID) (*pendingDeletionInfo, error) {
	const q = `SELECT deletion_requested_at, deletion_canceled_at FROM users WHERE id = $1`
	var requestedAt *time.Time
	var canceledAt *time.Time
	if err := pool.QueryRow(ctx, q, userID).Scan(&requestedAt, &canceledAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// User got hard-deleted; treat as no-pending so the next
			// layer can write its own 401/404. Definitely not a 423.
			return nil, nil
		}
		return nil, err
	}
	if requestedAt == nil || canceledAt != nil {
		return nil, nil
	}
	return &pendingDeletionInfo{deletionRequestedAt: *requestedAt}, nil
}
