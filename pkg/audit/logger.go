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

// UserResolver returns the email-at-event for a given userID, or "" + error
// if the user cannot be resolved (e.g. deleted post-event). The audit
// logger calls this BEFORE the INSERT so the row survives the user being
// hard-deleted later (Phase 21 ACCT-06).
//
// Defined in this package (not domain) to avoid importing the API
// repository layer into pkg/audit. Production wiring (wire/services.go)
// passes a tiny adapter wrapping UserRepository.GetByID.
type UserResolver interface {
	EmailByID(ctx context.Context, userID uuid.UUID) (string, error)
}

// NopUserResolver always returns ("", nil). Used by NewLogger when no
// resolver is configured (backward compatibility for tests + pre-Phase-21
// call sites). Production wiring uses NewLoggerWithResolver.
type NopUserResolver struct{}

// EmailByID always returns ("", nil) — the no-op resolver.
func (NopUserResolver) EmailByID(context.Context, uuid.UUID) (string, error) {
	return "", nil
}

// NewLogger returns a Logger that persists to repo asynchronously, with
// bounded retry (D-06) and a fail-open metric increment (D-07).
//
// Backward-compatible — uses NopUserResolver so existing call sites keep
// working without touching user_email_at_event. Production wiring SHOULD
// use NewLoggerWithResolver (Phase 21 ACCT-06) to populate the
// user_email_at_event column for post-delete audit retention.
func NewLogger(repo domain.AuditLogRepository) Logger {
	return &loggerImpl{repo: repo, resolver: NopUserResolver{}}
}

// NewLoggerWithResolver returns a Logger that additionally populates
// AuditLog.UserEmailAtEvent on every write by calling resolver.EmailByID
// BEFORE the INSERT (Phase 21 ACCT-06). Resolver failure NEVER blocks
// the audit row — slog.Warn + empty string fallback.
//
// If resolver is nil, falls back to NopUserResolver (matches NewLogger
// semantics).
func NewLoggerWithResolver(repo domain.AuditLogRepository, resolver UserResolver) Logger {
	if resolver == nil {
		resolver = NopUserResolver{}
	}
	return &loggerImpl{repo: repo, resolver: resolver}
}

type loggerImpl struct {
	repo     domain.AuditLogRepository
	resolver UserResolver
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

	// ACCT-06: snapshot email at write-time so audit_logs survives user delete.
	// Resolver failure must NEVER block the audit row — log and leave
	// UserEmailAtEvent empty (the user_id FK will be NULLed on delete, so
	// we lose identity for this single event but preserve the action history).
	if e.UserID != nil {
		email, err := l.resolver.EmailByID(spawnCtx, *e.UserID)
		if err != nil {
			slog.WarnContext(reqCtx, "audit: user resolver failed",
				"user_id", e.UserID,
				"action", e.Action,
				"error", err,
			)
		} else {
			row.UserEmailAtEvent = email
		}
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
