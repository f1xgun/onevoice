// Package repository — email_outbox.go
//
// EmailOutboxRepository owns every SQL statement against email_outbox,
// the transactional outbox table introduced by Phase 21a (Account
// Lifecycle / Email Infrastructure). Per services/api/AGENTS.md
// layering, handlers and services do not query this table directly;
// downstream Phase 21 services (PasswordResetService,
// EmailVerificationService, AccountDeletionService) call Enqueue
// inside the SAME transaction that creates the originating row, then
// a background worker spawned in cmd/main.go drains pending rows via
// DrainPending / MarkSent / Reschedule / MarkFailed.
//
// Atomicity guarantees:
//   - Enqueue takes a pgx.Tx the CALLER controls. If the caller's
//     Commit fails, the email vanishes alongside the originating row.
//     No orphan emails are ever sent. TestEmailOutbox_Enqueue_Rollback
//     proves this end-to-end.
//   - MarkSent / Reschedule / MarkFailed all carry `WHERE id=$1 AND
//     status='pending'` so the worker's at-least-once semantics never
//     cause a double-update (T-INF-02 in the threat model). A row
//     that's already been marked is silently a no-op.
//
// Retry policy (D-07):
//   - Reschedule with currentAttempts == maxAttempts-1 (i.e. this is
//     the Nth attempt and it failed) transitions the row to
//     status='failed' instead of scheduling a retry.
//   - Backoff is NOW() + (2^newAttempts) minutes: 1m, 2m, 4m, 8m,
//     16m for newAttempts 0..4.
package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// outboxBackoffBase is the exponential-backoff base in minutes.
// nextAttempt = NOW() + (outboxBackoffBase ^ newAttempts) minutes.
// 2 gives 1m, 2m, 4m, 8m, 16m for newAttempts 0..4 — well within
// the 30-minute reset-token TTL so a worst-case 5-attempt retry
// still delivers before the token expires.
const outboxBackoffBase = 2

// outboxLastErrorMaxLen caps the last_error column to keep a single
// pathological Unisender response from bloating the table. 2000 chars
// is roughly 4 KB UTF-8, plenty for any realistic error.
const outboxLastErrorMaxLen = 2000

// OutboxRow is the worker's view of a pending email_outbox row.
type OutboxRow struct {
	ID        uuid.UUID
	ToEmail   string
	Subject   string
	BodyText  string
	BodyHTML  string
	Attempts  int
	CreatedAt time.Time
}

// OutboxEnqueueInput is the minimum a caller needs to schedule a send.
// BodyHTML may be empty for text-only mail.
type OutboxEnqueueInput struct {
	ToEmail  string
	Subject  string
	BodyText string
	BodyHTML string
}

// ErrEmailOutboxNotFound is a sentinel for callers that want to check
// "did this row exist at all?". Currently no repo method returns it —
// reserved for forward compatibility when a Get(id) accessor lands.
var ErrEmailOutboxNotFound = errors.New("email_outbox: row not found")

// EmailOutboxRepository owns every SQL statement against email_outbox.
// Both *pgxpool.Pool (production) and pgxmock.PgxPoolIface (unit tests)
// satisfy the constructor via the package-local pgxPool interface
// defined in pool.go.
type EmailOutboxRepository struct {
	pool pgxPool
}

// NewEmailOutboxRepository returns the Phase 21a concrete email_outbox
// repository. Returns the concrete type (not a domain interface) because
// the worker and downstream Phase 21b/21c/21d services depend on the
// methods directly — there is no need for the indirection.
func NewEmailOutboxRepository(pool pgxPool) *EmailOutboxRepository {
	return &EmailOutboxRepository{pool: pool}
}

// Enqueue inserts a pending row INSIDE the caller's transaction. This
// is the cornerstone of the transactional outbox: the originating row
// (e.g. password_reset_tokens) and its email are persisted atomically.
// If the caller rolls back tx, the outbox row vanishes too — no orphan
// email is ever sent for a transaction that didn't commit.
//
// The caller controls tx lifecycle (Begin/Commit/Rollback) — Enqueue
// never starts or ends a transaction.
//
// Phase 21-04 extension (21-CROSS-PLAN-CONTRACTS §3): when tx == nil,
// Enqueue falls back to a pool.QueryRow INSERT so sweeper-driven sends
// (e.g. the T-7 deletion warning sweeper) can enqueue without a
// surrounding business transaction. Atomicity is still preserved in the
// tx-using cases (password reset, verification, deletion request).
func (r *EmailOutboxRepository) Enqueue(ctx context.Context, tx pgx.Tx, in OutboxEnqueueInput) (uuid.UUID, error) {
	const q = `
		INSERT INTO email_outbox (to_email, subject, body_text, body_html)
		VALUES ($1, $2, $3, $4)
		RETURNING id`
	var id uuid.UUID
	if tx == nil {
		// Phase 21-04: sweeper path with no business tx. Falls back to
		// pool — single statement is atomic at the row level (no other row
		// is involved). Safe because the dedupe check in the caller (e.g.
		// ExistsBySubjectAndRecipient) precedes this call.
		if err := r.pool.QueryRow(ctx, q, in.ToEmail, in.Subject, in.BodyText, in.BodyHTML).Scan(&id); err != nil {
			return uuid.Nil, fmt.Errorf("email_outbox: enqueue (nil tx): %w", err)
		}
		return id, nil
	}
	if err := tx.QueryRow(ctx, q, in.ToEmail, in.Subject, in.BodyText, in.BodyHTML).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("email_outbox: enqueue: %w", err)
	}
	return id, nil
}

// EnqueueDeferred — Phase 21-04 extension (21-CROSS-PLAN-CONTRACTS §2a).
// Same as Enqueue but writes an explicit `next_attempt_at` so the worker
// won't pick the row up until then. Used for the T-7 deletion warning
// email (23 days in the future at request-deletion time).
//
// Also accepts a nil tx (sweeper fallback path), mirroring Enqueue.
func (r *EmailOutboxRepository) EnqueueDeferred(ctx context.Context, tx pgx.Tx, in OutboxEnqueueInput, nextAttemptAt time.Time) (uuid.UUID, error) {
	const q = `
		INSERT INTO email_outbox (to_email, subject, body_text, body_html, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`
	var id uuid.UUID
	if tx == nil {
		if err := r.pool.QueryRow(ctx, q, in.ToEmail, in.Subject, in.BodyText, in.BodyHTML, nextAttemptAt).Scan(&id); err != nil {
			return uuid.Nil, fmt.Errorf("email_outbox: enqueue deferred (nil tx): %w", err)
		}
		return id, nil
	}
	if err := tx.QueryRow(ctx, q, in.ToEmail, in.Subject, in.BodyText, in.BodyHTML, nextAttemptAt).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("email_outbox: enqueue deferred: %w", err)
	}
	return id, nil
}

// ExistsBySubjectAndRecipient — Phase 21-04 extension (21-CROSS-PLAN-CONTRACTS §2b).
// Returns true if at least one email_outbox row exists for (to_email,
// subject) in ANY status (pending|sent|failed|canceled). Used by the
// deletion-warning sweeper to dedupe: a single user must receive at most
// ONE T-7 reminder no matter how many times the sweeper runs.
func (r *EmailOutboxRepository) ExistsBySubjectAndRecipient(ctx context.Context, toEmail, subject string) (bool, error) {
	const q = `SELECT EXISTS (
		SELECT 1 FROM email_outbox WHERE to_email = $1 AND subject = $2
	)`
	var exists bool
	if err := r.pool.QueryRow(ctx, q, toEmail, subject).Scan(&exists); err != nil {
		return false, fmt.Errorf("email_outbox: exists check: %w", err)
	}
	return exists, nil
}

// CancelPendingBySubjectAndRecipient — Phase 21-04. When a user cancels
// their pending deletion via POST /users/me/restore, the pending T-7
// warning row (scheduled +23d in the future via EnqueueDeferred) should
// be canceled so the user doesn't receive the warning after restoring.
// Idempotent: returns nil even if 0 rows match (no pending row OR worker
// already failed-out the row).
func (r *EmailOutboxRepository) CancelPendingBySubjectAndRecipient(ctx context.Context, toEmail, subject string) error {
	const q = `UPDATE email_outbox
	              SET status = 'canceled',
	                  last_error = 'canceled: deletion restored'
	            WHERE to_email = $1 AND subject = $2 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, toEmail, subject); err != nil {
		return fmt.Errorf("email_outbox: cancel pending: %w", err)
	}
	return nil
}

// DrainPending returns up to `limit` rows where status='pending' AND
// next_attempt_at <= NOW(), ordered by next_attempt_at ASC (oldest
// first). The worker iterates the returned slice and calls Sender.Send
// for each.
//
// No row-level locking: the worker is a single goroutine per process.
// If we ever run multiple API replicas drained concurrently, switch to
// SELECT ... FOR UPDATE SKIP LOCKED here.
func (r *EmailOutboxRepository) DrainPending(ctx context.Context, limit int) ([]OutboxRow, error) {
	const q = `
		SELECT id, to_email, subject, body_text, COALESCE(body_html, ''), attempts, created_at
		FROM email_outbox
		WHERE status = 'pending'
		  AND next_attempt_at <= NOW()
		ORDER BY next_attempt_at ASC
		LIMIT $1`
	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("email_outbox: drain query: %w", err)
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		var row OutboxRow
		if err := rows.Scan(&row.ID, &row.ToEmail, &row.Subject, &row.BodyText, &row.BodyHTML, &row.Attempts, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("email_outbox: drain scan: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("email_outbox: drain rows: %w", err)
	}
	return out, nil
}

// MarkSent transitions a row to status='sent' atomically. The WHERE
// clause `AND status='pending'` makes this a no-op if a concurrent
// worker (or a previous boot's interrupted MarkSent) already marked
// it (T-INF-02: at-least-once, never-twice from the DB's perspective).
//
// attempts is incremented so MarkSent + the existing 0-attempts default
// captures one "this delivery succeeded" entry on the success path.
func (r *EmailOutboxRepository) MarkSent(ctx context.Context, id uuid.UUID, providerJobID string) error {
	const q = `
		UPDATE email_outbox
		   SET status = 'sent',
		       sent_at = NOW(),
		       last_error = NULL,
		       attempts = attempts + 1
		 WHERE id = $1 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("email_outbox: mark sent: %w", err)
	}
	// Note: we deliberately discard providerJobID — the column is not
	// stored today. Phase 21d may add a provider_job_id column when
	// support tooling needs cross-referencing with the Unisender dashboard.
	_ = providerJobID
	// We intentionally do not error on "no rows affected" — that means
	// a concurrent boot/worker already marked it sent. Idempotent.
	return nil
}

// Reschedule increments attempts and bumps next_attempt_at by
// exp-backoff. When attempts reaches maxAttempts, the row transitions
// to status='failed' instead of being rescheduled (D-07).
//
// Backoff formula: NOW() + (2 ^ newAttempts) minutes.
//
//	currentAttempts=0 → newAttempts=1, wait  2m
//	currentAttempts=1 → newAttempts=2, wait  4m
//	currentAttempts=2 → newAttempts=3, wait  8m
//	currentAttempts=3 → newAttempts=4, wait 16m
//	currentAttempts=4 → at cap → status='failed'
func (r *EmailOutboxRepository) Reschedule(ctx context.Context, id uuid.UUID, currentAttempts int, lastErr string, maxAttempts int) error {
	newAttempts := currentAttempts + 1
	if newAttempts >= maxAttempts {
		const q = `
			UPDATE email_outbox
			   SET status = 'failed',
			       attempts = $2,
			       last_error = $3
			 WHERE id = $1 AND status = 'pending'`
		if _, err := r.pool.Exec(ctx, q, id, newAttempts, truncateOutboxErr(lastErr)); err != nil {
			return fmt.Errorf("email_outbox: reschedule->failed: %w", err)
		}
		return nil
	}
	backoff := time.Duration(math.Pow(outboxBackoffBase, float64(newAttempts))) * time.Minute
	const q = `
		UPDATE email_outbox
		   SET attempts = $2,
		       last_error = $3,
		       next_attempt_at = NOW() + ($4::interval)
		 WHERE id = $1 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, id, newAttempts, truncateOutboxErr(lastErr), fmt.Sprintf("%d seconds", int(backoff.Seconds()))); err != nil {
		return fmt.Errorf("email_outbox: reschedule: %w", err)
	}
	return nil
}

// MarkFailed forces a row to status='failed' immediately (permanent
// failure path — UnisenderSender returned ErrPermanent). The
// `AND status='pending'` guard means a concurrent MarkSent wins,
// preserving the "successful delivery is final" invariant.
func (r *EmailOutboxRepository) MarkFailed(ctx context.Context, id uuid.UUID, lastErr string) error {
	const q = `
		UPDATE email_outbox
		   SET status = 'failed',
		       attempts = attempts + 1,
		       last_error = $2
		 WHERE id = $1 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, id, truncateOutboxErr(lastErr)); err != nil {
		return fmt.Errorf("email_outbox: mark failed: %w", err)
	}
	return nil
}

// truncateOutboxErr bounds the last_error column to keep a single
// pathological Unisender response from bloating the table.
func truncateOutboxErr(s string) string {
	if len(s) <= outboxLastErrorMaxLen {
		return s
	}
	return s[:outboxLastErrorMaxLen] + "..."
}
