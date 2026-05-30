package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// stubRepo is an in-memory AuditLogRepository for logger tests.
// failTimes is the number of Insert calls that should fail before a
// success — used to exercise the retry path.
type stubRepo struct {
	mu        sync.Mutex
	inserted  []*domain.AuditLog
	failTimes atomic.Int32
}

func (s *stubRepo) Insert(_ context.Context, log *domain.AuditLog) error {
	if s.failTimes.Load() > 0 {
		s.failTimes.Add(-1)
		return fmt.Errorf("simulated failure")
	}
	s.mu.Lock()
	s.inserted = append(s.inserted, log)
	s.mu.Unlock()
	return nil
}

func (s *stubRepo) ListByBusiness(context.Context, uuid.UUID, domain.AuditLogFilter) ([]domain.AuditLog, error) {
	return nil, errors.New("not implemented")
}

func (s *stubRepo) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}

// waitForInserts polls until the stub has the expected number of inserts
// or the deadline elapses. Returns the observed count.
func waitForInserts(repo *stubRepo, want int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		repo.mu.Lock()
		n := len(repo.inserted)
		repo.mu.Unlock()
		if n >= want {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return len(repo.inserted)
}

func TestLogger_HappyPath(t *testing.T) {
	repo := &stubRepo{}
	l := NewLogger(repo)
	bizID := uuid.New()
	l.Log(context.Background(), Entry{
		Action:     ActionBusinessCreated,
		Resource:   "business",
		BusinessID: &bizID,
		Details:    json.RawMessage(`{"name":"acme"}`),
	})
	require.Equal(t, 1, waitForInserts(repo, 1, 2*time.Second))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, ActionBusinessCreated, repo.inserted[0].Action)
	require.Equal(t, "business", repo.inserted[0].Resource)
	require.NotNil(t, repo.inserted[0].BusinessID)
	require.Equal(t, bizID, *repo.inserted[0].BusinessID)
}

func TestLogger_RetriesThenSucceeds(t *testing.T) {
	repo := &stubRepo{}
	repo.failTimes.Store(2) // first 2 attempts fail, 3rd succeeds
	l := NewLogger(repo)
	l.Log(context.Background(), Entry{Action: ActionLoginSuccess, Resource: "user"})
	// 1s + 2s + jitter ≈ <4s; allow generous deadline.
	require.Equal(t, 1, waitForInserts(repo, 1, 6*time.Second))
}

func TestLogger_TerminalFailureIncrementsMetric(t *testing.T) {
	repo := &stubRepo{}
	repo.failTimes.Store(10) // exceeds 3 attempts → terminal
	before := testutil.ToFloat64(auditLogWriteFailuresTotal.WithLabelValues("auth"))
	l := NewLogger(repo)
	l.Log(context.Background(), Entry{Action: ActionLoginFailed, Resource: "user"})
	// Wait for all 3 retries + backoffs (1s + 2s + jitter ≈ ~3-5s).
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if testutil.ToFloat64(auditLogWriteFailuresTotal.WithLabelValues("auth")) >= before+1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	after := testutil.ToFloat64(auditLogWriteFailuresTotal.WithLabelValues("auth"))
	require.Equal(t, before+1, after, "expected one failure increment on auth category")
}

// --- / — UserResolver populates user_email_at_event ---

// fakeUserResolver is a programmable stub for the EmailByID lookup. If
// errResult is non-nil it returns ("", errResult); otherwise (emailResult, nil).
type fakeUserResolver struct {
	emailResult string
	errResult   error
	calls       atomic.Int32
}

func (f *fakeUserResolver) EmailByID(_ context.Context, _ uuid.UUID) (string, error) {
	f.calls.Add(1)
	if f.errResult != nil {
		return "", f.errResult
	}
	return f.emailResult, nil
}

func TestLogger_WithResolver_PopulatesEmail(t *testing.T) {
	repo := &stubRepo{}
	resolver := &fakeUserResolver{emailResult: "alice@example.com"}
	l := NewLoggerWithResolver(repo, resolver)
	uid := uuid.New()
	l.Log(context.Background(), Entry{
		Action:   ActionLoginSuccess,
		Resource: "user",
		UserID:   &uid,
	})
	require.Equal(t, 1, waitForInserts(repo, 1, 2*time.Second))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, "alice@example.com", repo.inserted[0].UserEmailAtEvent)
	require.Equal(t, int32(1), resolver.calls.Load())
}

func TestLogger_WithResolver_NilUserIDLeavesEmailEmpty(t *testing.T) {
	repo := &stubRepo{}
	resolver := &fakeUserResolver{emailResult: "should-not-be-used@x.com"}
	l := NewLoggerWithResolver(repo, resolver)
	l.Log(context.Background(), Entry{
		Action:   ActionLoginFailed, // typical nil-userID entry
		Resource: "user",
	})
	require.Equal(t, 1, waitForInserts(repo, 1, 2*time.Second))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, "", repo.inserted[0].UserEmailAtEvent)
	require.Equal(t, int32(0), resolver.calls.Load(), "resolver MUST NOT be called when UserID is nil")
}

func TestLogger_WithResolver_ErrorStillWritesRow(t *testing.T) {
	repo := &stubRepo{}
	resolver := &fakeUserResolver{errResult: errors.New("simulated resolver error")}
	l := NewLoggerWithResolver(repo, resolver)
	uid := uuid.New()
	l.Log(context.Background(), Entry{
		Action:   ActionEmailVerified,
		Resource: "user",
		UserID:   &uid,
	})
	require.Equal(t, 1, waitForInserts(repo, 1, 2*time.Second))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, "", repo.inserted[0].UserEmailAtEvent, "resolver error must leave email empty but still write the row")
	require.Equal(t, int32(1), resolver.calls.Load())
}

func TestLogger_NewLoggerKeepsNopResolver(t *testing.T) {
	// Backward-compatibility gate: NewLogger (no resolver) must continue to
	// produce empty UserEmailAtEvent without calling any resolver. Defends
	// against a future refactor accidentally requiring resolver injection.
	repo := &stubRepo{}
	l := NewLogger(repo)
	uid := uuid.New()
	l.Log(context.Background(), Entry{
		Action:   ActionLoginSuccess,
		Resource: "user",
		UserID:   &uid,
	})
	require.Equal(t, 1, waitForInserts(repo, 1, 2*time.Second))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, "", repo.inserted[0].UserEmailAtEvent)
}
