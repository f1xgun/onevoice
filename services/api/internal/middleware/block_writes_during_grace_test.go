package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// fakePool implements graceQueryRunner via a closure so each test can
// scope its expected return cleanly.
type fakePool struct {
	scan func(args ...any) error
}

type fakeRow struct {
	scan func(args ...any) error
}

func (f fakeRow) Scan(dest ...any) error { return f.scan(dest...) }

func (p *fakePool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return fakeRow{scan: p.scan}
}


// inject the user-id into context BEFORE the middleware runs, then
// chain the middleware over a trivial 200-OK handler so we can observe
// whether the middleware short-circuited.
func mkRequestWithUser(t *testing.T, method string, userID uuid.UUID) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, "/anything", nil)
	ctx := context.WithValue(req.Context(), UserIDKey, userID)
	return req.WithContext(ctx)
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("inner-ok"))
	})
}

func TestBlockWrites_AllowsReadsDuringGrace(t *testing.T) {
	userID := uuid.New()
	pool := &fakePool{
		scan: func(args ...any) error {
			t.Fatal("DB should NOT be hit on GET — read path bypasses")
			return nil
		},
	}
	mw := BlockWritesDuringGrace(pool, 30)
	req := mkRequestWithUser(t, http.MethodGet, userID)
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "inner-ok", rec.Body.String())
}

func TestBlockWrites_PassesThrough_NoPending(t *testing.T) {
	userID := uuid.New()
	pool := &fakePool{
		scan: func(args ...any) error {
			// no pending — both ptrs stay nil
			return nil
		},
	}
	mw := BlockWritesDuringGrace(pool, 30)
	req := mkRequestWithUser(t, http.MethodPost, userID)
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "inner-ok", rec.Body.String())
}

func TestBlockWrites_Returns423_WhenPending(t *testing.T) {
	userID := uuid.New()
	requestedAt := time.Now().Add(-2 * 24 * time.Hour) // 2 days into grace
	pool := &fakePool{
		scan: func(args ...any) error {
			// args[0] = *deletion_requested_at, args[1] = *deletion_canceled_at
			req, ok := args[0].(**time.Time)
			require.True(t, ok)
			*req = &requestedAt
			return nil
		},
	}
	mw := BlockWritesDuringGrace(pool, 30)
	req := mkRequestWithUser(t, http.MethodPost, userID)
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusLocked, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"code":"account_pending_deletion"`)
	require.Contains(t, body, `"restoreUrl":"/settings/account"`)
	require.Contains(t, body, `"deletionDate":`)
}

func TestBlockWrites_PassesThrough_WhenCanceled(t *testing.T) {
	userID := uuid.New()
	requestedAt := time.Now().Add(-2 * 24 * time.Hour)
	canceledAt := time.Now().Add(-1 * 24 * time.Hour)
	pool := &fakePool{
		scan: func(args ...any) error {
			req, _ := args[0].(**time.Time)
			can, _ := args[1].(**time.Time)
			*req = &requestedAt
			*can = &canceledAt
			return nil
		},
	}
	mw := BlockWritesDuringGrace(pool, 30)
	req := mkRequestWithUser(t, http.MethodPost, userID)
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBlockWrites_FailsOpen_OnDBError(t *testing.T) {
	userID := uuid.New()
	pool := &fakePool{
		scan: func(args ...any) error {
			return errors.New("db down")
		},
	}
	mw := BlockWritesDuringGrace(pool, 30)
	req := mkRequestWithUser(t, http.MethodPost, userID)
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "fail-open on DB hiccup (T-DEL-11)")
	require.Equal(t, "inner-ok", rec.Body.String())
}

func TestBlockWrites_PassesThrough_OnNoRows(t *testing.T) {
	userID := uuid.New()
	pool := &fakePool{
		scan: func(args ...any) error {
			return pgx.ErrNoRows
		},
	}
	mw := BlockWritesDuringGrace(pool, 30)
	req := mkRequestWithUser(t, http.MethodPost, userID)
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	// User hard-deleted — should pass through (Auth would 401 elsewhere).
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBlockWrites_NoAuthCtx_PassesThrough(t *testing.T) {
	pool := &fakePool{
		scan: func(args ...any) error {
			t.Fatal("DB should NOT be hit when there is no user context")
			return nil
		},
	}
	mw := BlockWritesDuringGrace(pool, 30)
	req := httptest.NewRequest(http.MethodPost, "/anything", nil)
	// no userID in context — Auth would have caught earlier
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestBlockWrites_DeletionDateMath confirms the body's deletionDate is
// computed as requestedAt + graceDays. Pinning the formula catches
// drift if the const ever changes without updating the middleware.
func TestBlockWrites_DeletionDateMath(t *testing.T) {
	userID := uuid.New()
	requestedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	pool := &fakePool{
		scan: func(args ...any) error {
			req, _ := args[0].(**time.Time)
			*req = &requestedAt
			return nil
		},
	}
	mw := BlockWritesDuringGrace(pool, 30)
	req := mkRequestWithUser(t, http.MethodPost, userID)
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusLocked, rec.Code)
	// Expected: 2026-07-01T12:00:00Z
	require.True(t, strings.Contains(rec.Body.String(), `"deletionDate":"2026-07-01T12:00:00Z"`),
		"deletionDate math drift; got %s", rec.Body.String())
}
