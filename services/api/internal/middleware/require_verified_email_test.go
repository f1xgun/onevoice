package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// fakeUserLookup is a programmable UserLookup stub.
type fakeUserLookup struct {
	user *domain.User
	err  error
	cnt  atomic.Int32
}

func (f *fakeUserLookup) GetByID(_ context.Context, _ uuid.UUID) (*domain.User, error) {
	f.cnt.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

// withUserID injects a userID into the request context the way the Auth
// middleware would.
func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	ctx := context.WithValue(r.Context(), UserIDKey, id)
	return r.WithContext(ctx)
}

// nextOK records that the downstream handler ran and writes 200.
func nextOK(t *testing.T, called *bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

// --- Day0 ---------------------------------------------------------------

func TestRequireVerifiedEmailDay0_BlocksUnverifiedRegardlessOfAge(t *testing.T) {
	uid := uuid.New()
	users := &fakeUserLookup{
		user: &domain.User{
			ID:            uid,
			Email:         "u@x.com",
			EmailVerified: false,
			CreatedAt:     time.Now().Add(-24 * time.Hour), // 1 day old — still blocks at day 0
		},
	}
	var called bool
	h := RequireVerifiedEmailDay0(users)(nextOK(t, &called))

	req := withUserID(httptest.NewRequest(http.MethodPost, "/api/v1/integrations/telegram/connect", http.NoBody), uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.False(t, called, "downstream handler must NOT run when unverified")
	require.Equal(t, http.StatusPreconditionFailed, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "email_verification_required", body["code"])
	require.Contains(t, body, "verifiedDeadline")
}

func TestRequireVerifiedEmailDay0_AllowsVerified(t *testing.T) {
	uid := uuid.New()
	users := &fakeUserLookup{
		user: &domain.User{
			ID:            uid,
			Email:         "u@x.com",
			EmailVerified: true,
			CreatedAt:     time.Now().Add(-24 * time.Hour),
		},
	}
	var called bool
	h := RequireVerifiedEmailDay0(users)(nextOK(t, &called))

	req := withUserID(httptest.NewRequest(http.MethodPost, "/api/v1/integrations/telegram/connect", http.NoBody), uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

// --- Day7 ---------------------------------------------------------------

func TestRequireVerifiedEmailDay7_AllowsUnverifiedWithinGrace(t *testing.T) {
	uid := uuid.New()
	users := &fakeUserLookup{
		user: &domain.User{
			ID:            uid,
			Email:         "u@x.com",
			EmailVerified: false,
			CreatedAt:     time.Now().Add(-3 * 24 * time.Hour), // 3 days old — within grace
		},
	}
	var called bool
	h := RequireVerifiedEmailDay7(users)(nextOK(t, &called))

	req := withUserID(httptest.NewRequest(http.MethodPost, "/api/v1/businesses", http.NoBody), uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.True(t, called, "day-7 middleware must pass through during grace")
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireVerifiedEmailDay7_BlocksUnverifiedPastGrace(t *testing.T) {
	uid := uuid.New()
	users := &fakeUserLookup{
		user: &domain.User{
			ID:            uid,
			Email:         "u@x.com",
			EmailVerified: false,
			CreatedAt:     time.Now().Add(-8 * 24 * time.Hour), // 8 days old — past grace
		},
	}
	var called bool
	h := RequireVerifiedEmailDay7(users)(nextOK(t, &called))

	req := withUserID(httptest.NewRequest(http.MethodPost, "/api/v1/businesses", http.NoBody), uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusPreconditionFailed, w.Code)
}

func TestRequireVerifiedEmailDay7_AllowsVerifiedRegardlessOfAge(t *testing.T) {
	uid := uuid.New()
	users := &fakeUserLookup{
		user: &domain.User{
			ID:            uid,
			Email:         "u@x.com",
			EmailVerified: true,
			CreatedAt:     time.Now().Add(-100 * 24 * time.Hour),
		},
	}
	var called bool
	h := RequireVerifiedEmailDay7(users)(nextOK(t, &called))

	req := withUserID(httptest.NewRequest(http.MethodPost, "/api/v1/businesses", http.NoBody), uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

// --- Edge cases ---------------------------------------------------------

// TestRequireVerifiedEmail_NoUserIDPassesThrough — defense-in-depth pass-through
// when the upstream Auth middleware did not set a userID. The downstream
// handler is expected to 401 in this case.
func TestRequireVerifiedEmail_NoUserIDPassesThrough(t *testing.T) {
	users := &fakeUserLookup{} // never called
	var called bool
	h := RequireVerifiedEmailDay0(users)(nextOK(t, &called))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/telegram/connect", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, int32(0), users.cnt.Load(), "must not query DB when no userID")
}

// TestRequireVerifiedEmail_UserNotFoundReturns401 covers the post-deletion
// race (Phase 21-04 hard-deletes the user mid-flight).
func TestRequireVerifiedEmail_UserNotFoundReturns401(t *testing.T) {
	uid := uuid.New()
	users := &fakeUserLookup{err: domain.ErrUserNotFound}
	var called bool
	h := RequireVerifiedEmailDay0(users)(nextOK(t, &called))

	req := withUserID(httptest.NewRequest(http.MethodPost, "/api/v1/integrations/telegram/connect", http.NoBody), uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireVerifiedEmail_SingleDBQueryPerRequest is the perf-regression
// guard (T-VE-03 mitigation: the DB lookup is intentional + bounded to ONE
// round-trip per request).
func TestRequireVerifiedEmail_SingleDBQueryPerRequest(t *testing.T) {
	uid := uuid.New()
	users := &fakeUserLookup{
		user: &domain.User{ID: uid, EmailVerified: true, CreatedAt: time.Now()},
	}
	var called bool
	h := RequireVerifiedEmailDay0(users)(nextOK(t, &called))

	req := withUserID(httptest.NewRequest(http.MethodPost, "/api/v1/integrations", http.NoBody), uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, int32(1), users.cnt.Load(), "exactly ONE DB query per protected request")
}
