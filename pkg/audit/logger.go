package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Entry is the value passed to Logger.Log. Details is pre-marshaled JSON so
// the typed builders own the marshaling step (D-10: no map[string]any).
type Entry struct {
	Action     string
	Resource   string
	BusinessID *uuid.UUID
	UserID     *uuid.UUID
	Details    json.RawMessage
}

// Logger is the public audit-write surface. The single Log method is
// intentionally non-blocking and non-error-returning (D-05 fire-and-forget).
//
// Callers do not need to handle errors — terminal failures (3 retries
// exhausted) increment audit_log_write_failures_total and log via slog.
type Logger interface {
	Log(ctx context.Context, e Entry)
}

// NewLogger returns a Logger that persists to repo asynchronously, with
// bounded retry (D-06) and a fail-open metric increment (D-07).
func NewLogger(repo domain.AuditLogRepository) Logger {
	return &loggerImpl{repo: repo}
}

type loggerImpl struct {
	repo domain.AuditLogRepository
}

const (
	writeAttempts       = 3
	writeAttemptTimeout = 30 * time.Second
)

// Log spawns a goroutine that persists the entry asynchronously. The
// request context is passed in for slog correlation only; the actual DB
// write uses a detached context.Background()-derived ctx so a
// response-time cancellation does not abort the write (Pitfall 1).
func (l *loggerImpl) Log(ctx context.Context, e Entry) {
	go l.write(ctx, e)
}

func (l *loggerImpl) write(reqCtx context.Context, e Entry) {
	spawnCtx, cancel := context.WithTimeout(context.Background(), writeAttemptTimeout)
	defer cancel()

	row := &domain.AuditLog{
		BusinessID: e.BusinessID,
		UserID:     e.UserID,
		Action:     e.Action,
		Resource:   e.Resource,
		Details:    e.Details,
		// ID + CreatedAt are filled by the DB
		// (DEFAULT gen_random_uuid() / now()).
	}

	var lastErr error
	for attempt := 0; attempt < writeAttempts; attempt++ {
		if attempt > 0 {
			sleep := time.Duration(1<<(attempt-1))*time.Second + jitter()
			select {
			case <-spawnCtx.Done():
				lastErr = spawnCtx.Err()
				l.fail(reqCtx, e, lastErr)
				return
			case <-time.After(sleep):
			}
		}
		if err := l.repo.Insert(spawnCtx, row); err == nil {
			return // success
		} else {
			lastErr = err
		}
	}

	l.fail(reqCtx, e, lastErr)
}

// fail records a terminal failure: increments the metric + slog. NEVER
// includes e.Details in the slog attrs — Details may contain emails / IPs
// per D-07.
func (l *loggerImpl) fail(reqCtx context.Context, e Entry, lastErr error) {
	IncWriteFailure(e.Action)
	// Use the REQUEST ctx for slog so the correlation_id stays attached.
	// NEVER log e.Details — it may contain emails / IPs per D-07.
	slog.ErrorContext(reqCtx, "audit log write failed",
		"action", e.Action,
		"business_id", e.BusinessID,
		"user_id", e.UserID,
		"error", lastErr,
	)
}

// jitter returns [0, 1s) — added to the exponential backoff so concurrent
// retries don't thunder against the DB after a brief outage. Weak random
// is intentional: this is timing jitter, not a security primitive.
func jitter() time.Duration {
	return time.Duration(rand.Int64N(int64(time.Second))) //nolint:gosec // weak random is intentional for backoff jitter (parity with services/agent-yandex-business humanDelay)
}

// Nop returns a Logger that discards every entry. Intended for unit tests
// that exercise handlers / services through the audit-injection seam without
// asserting on audit emission. Production wiring MUST use NewLogger(repo).
func Nop() Logger { return nopLogger{} }

type nopLogger struct{}

func (nopLogger) Log(context.Context, Entry) {}
