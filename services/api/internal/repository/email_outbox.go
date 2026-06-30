// Package repository — email_outbox.go.
//
// EmailOutboxRepository owns every SQL statement against the email_outbox
// transactional outbox table. See docs/api/repositories/email-outbox.md.
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
// nextAttempt = NOW + (outboxBackoffBase ^ newAttempts) minutes.
// Base 2 yields 1m, 2m, 4m, 8m, 16m for newAttempts 0..4 — within the
// 30-minute reset-token TTL so a worst-case 5-attempt retry still delivers.
const outboxBackoffBase = 2

// outboxLastErrorMaxLen caps last_error column (~4 KB UTF-8) so one
// pathological Unisender response can't bloat the table.
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
// BodyHTML may be empty for text-only mail. BusinessID scopes a row to one
// organization so the organization-deletion cancel can target a single pending
// T-7 warning when an owner has multiple pending deletions; it is nil (column
// NULL) for the single-per-recipient flows (account deletion, password reset,
// email verification, feedback).
type OutboxEnqueueInput struct {
	ToEmail    string
	Subject    string
	BodyText   string
	BodyHTML   string
	BusinessID *uuid.UUID
}

// ErrEmailOutboxNotFound is a forward-compatibility sentinel for a future
// Get(id) accessor; no method returns it today.
var ErrEmailOutboxNotFound = errors.New("email_outbox: row not found")

// EmailOutboxRepository owns every SQL statement against email_outbox.
// See docs/api/repositories/email-outbox.md.
type EmailOutboxRepository struct {
	pool pgxPool
}

// NewEmailOutboxRepository returns the concrete email_outbox repository.
// Concrete type (not interface) because the worker and lifecycle services
// depend on the methods directly.
func NewEmailOutboxRepository(pool pgxPool) *EmailOutboxRepository {
	return &EmailOutboxRepository{pool: pool}
}

// Enqueue inserts a pending row INSIDE the caller's transaction so the
// originating row and its email persist atomically. If tx == nil, falls back
// to pool.QueryRow for sweeper paths with no surrounding business tx.
func (r *EmailOutboxRepository) Enqueue(ctx context.Context, tx pgx.Tx, in OutboxEnqueueInput) (uuid.UUID, error) {
	const q = `
		INSERT INTO email_outbox (to_email, subject, body_text, body_html, business_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`
	var id uuid.UUID
	if tx == nil {
		if err := r.pool.QueryRow(ctx, q, in.ToEmail, in.Subject, in.BodyText, in.BodyHTML, in.BusinessID).Scan(&id); err != nil {
			return uuid.Nil, fmt.Errorf("email_outbox: enqueue (nil tx): %w", err)
		}
		return id, nil
	}
	if err := tx.QueryRow(ctx, q, in.ToEmail, in.Subject, in.BodyText, in.BodyHTML, in.BusinessID).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("email_outbox: enqueue: %w", err)
	}
	return id, nil
}

// EnqueueDeferred is Enqueue plus an explicit next_attempt_at so the worker
// won't pick the row up until then (T-7 deletion warning: +23d).
// Also accepts a nil tx (sweeper fallback path), mirroring Enqueue.
func (r *EmailOutboxRepository) EnqueueDeferred(ctx context.Context, tx pgx.Tx, in OutboxEnqueueInput, nextAttemptAt time.Time) (uuid.UUID, error) {
	const q = `
		INSERT INTO email_outbox (to_email, subject, body_text, body_html, next_attempt_at, business_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`
	var id uuid.UUID
	if tx == nil {
		if err := r.pool.QueryRow(ctx, q, in.ToEmail, in.Subject, in.BodyText, in.BodyHTML, nextAttemptAt, in.BusinessID).Scan(&id); err != nil {
			return uuid.Nil, fmt.Errorf("email_outbox: enqueue deferred (nil tx): %w", err)
		}
		return id, nil
	}
	if err := tx.QueryRow(ctx, q, in.ToEmail, in.Subject, in.BodyText, in.BodyHTML, nextAttemptAt, in.BusinessID).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("email_outbox: enqueue deferred: %w", err)
	}
	return id, nil
}

// ExistsBySubjectAndRecipient returns true if at least one email_outbox row
// exists for (to_email, subject) in any status. Used by sweeper dedupe so a
// user receives at most one T-7 reminder regardless of sweeper cadence.
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

// CancelPendingBySubjectAndRecipient transitions a pending row to 'canceled'
// when a user restores their account, so the +23d T-7 warning doesn't send.
// Idempotent: nil even when 0 rows match. 'canceled' is terminal, so the same
// UPDATE scrubs body_text/body_html for consistency with the other terminal
// transitions (subject + to_email are kept for audit/dedupe).
func (r *EmailOutboxRepository) CancelPendingBySubjectAndRecipient(ctx context.Context, toEmail, subject string) error {
	const q = `UPDATE email_outbox
	              SET status = 'canceled',
	                  last_error = 'canceled: deletion restored',
	                  body_text = '',
	                  body_html = NULL
	            WHERE to_email = $1 AND subject = $2 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, toEmail, subject); err != nil {
		return fmt.Errorf("email_outbox: cancel pending: %w", err)
	}
	return nil
}

// CancelPendingBusinessT7ByRecipient transitions a pending row to 'canceled'
// when an owner restores ONE organization, scoped to that organization's
// business_id so a sibling organization's still-scheduled T-7 warning (same
// to_email + subject, different business_id) is left untouched. Without the
// business_id predicate, restoring one of two pending organization deletions
// would cancel both rows and silently drop the other's advance-notice email.
// Idempotent: nil even when 0 rows match. Scrubs body for terminal-state
// consistency with the other transitions.
func (r *EmailOutboxRepository) CancelPendingBusinessT7ByRecipient(ctx context.Context, toEmail, subject string, businessID uuid.UUID) error {
	const q = `UPDATE email_outbox
	              SET status = 'canceled',
	                  last_error = 'canceled: deletion restored',
	                  body_text = '',
	                  body_html = NULL
	            WHERE to_email = $1 AND subject = $2 AND business_id = $3 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, toEmail, subject, businessID); err != nil {
		return fmt.Errorf("email_outbox: cancel pending business T-7: %w", err)
	}
	return nil
}

// DrainPending returns up to `limit` rows where status='pending' AND
// next_attempt_at <= NOW, ordered oldest-first. Single-worker assumption —
// switch to FOR UPDATE SKIP LOCKED if multiple replicas drain concurrently.
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

// CountPending returns the number of rows still in status='pending',
// regardless of next_attempt_at. Used to sample the outbox backlog gauge each
// worker tick; backoff-deferred rows are intentionally included so the gauge
// reflects total undelivered mail, not just the due slice.
func (r *EmailOutboxRepository) CountPending(ctx context.Context) (int, error) {
	const q = `SELECT COUNT(*) FROM email_outbox WHERE status = 'pending'`
	var n int
	if err := r.pool.QueryRow(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("email_outbox: count pending: %w", err)
	}
	return n, nil
}

// MarkSent transitions a row to status='sent' atomically. The WHERE clause
// `AND status='pending'` makes this idempotent under at-least-once worker
// semantics — a concurrent winner becomes a silent no-op for the loser.
//
// The same UPDATE scrubs body_text/body_html: a delivered row may embed a
// live plaintext recovery link (?token=…) whose token table stores only the
// SHA-256 hash, so the outbox must not be the one place the usable secret
// lingers at rest. 'sent' is terminal (no transition leaves it), so no retry
// re-reads the body; subject + to_email are kept for audit/dedupe.
func (r *EmailOutboxRepository) MarkSent(ctx context.Context, id uuid.UUID, providerJobID string) error {
	const q = `
		UPDATE email_outbox
		   SET status = 'sent',
		       sent_at = NOW(),
		       last_error = NULL,
		       attempts = attempts + 1,
		       body_text = '',
		       body_html = NULL
		 WHERE id = $1 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("email_outbox: mark sent: %w", err)
	}
	_ = providerJobID
	return nil
}

// Reschedule increments attempts + bumps next_attempt_at by exp-backoff.
// When attempts hits maxAttempts the row transitions to 'failed' instead.
// Backoff: NOW + (2 ^ newAttempts) minutes.
//
// The retry path leaves body_text/body_html intact (a later attempt re-reads
// them), but the terminal 'failed' branch scrubs them so a non-delivered row
// doesn't keep a usable recovery token at rest.
func (r *EmailOutboxRepository) Reschedule(ctx context.Context, id uuid.UUID, currentAttempts int, lastErr string, maxAttempts int) error {
	newAttempts := currentAttempts + 1
	if newAttempts >= maxAttempts {
		const q = `
			UPDATE email_outbox
			   SET status = 'failed',
			       attempts = $2,
			       last_error = $3,
			       body_text = '',
			       body_html = NULL
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

// RescheduleStrandedSent handles the row whose Send succeeded but whose
// MarkSent persist failed, leaving it in 'pending'. Unlike Reschedule it always
// scrubs body_text/body_html: delivery already happened, so the body — and the
// live recovery token it may carry — is no longer needed and must not linger at
// rest. It still bumps next_attempt_at by exp-backoff (so the row is not
// re-drained on the very next tick) and counts the attempt (so it eventually
// transitions to 'failed' at maxAttempts rather than re-sending forever).
func (r *EmailOutboxRepository) RescheduleStrandedSent(ctx context.Context, id uuid.UUID, currentAttempts, maxAttempts int) error {
	newAttempts := currentAttempts + 1
	if newAttempts >= maxAttempts {
		const q = `
			UPDATE email_outbox
			   SET status = 'failed',
			       attempts = $2,
			       last_error = 'stranded after successful delivery',
			       body_text = '',
			       body_html = NULL
			 WHERE id = $1 AND status = 'pending'`
		if _, err := r.pool.Exec(ctx, q, id, newAttempts); err != nil {
			return fmt.Errorf("email_outbox: reschedule stranded->failed: %w", err)
		}
		return nil
	}
	backoff := time.Duration(math.Pow(outboxBackoffBase, float64(newAttempts))) * time.Minute
	const q = `
		UPDATE email_outbox
		   SET attempts = $2,
		       last_error = 'stranded after successful delivery',
		       next_attempt_at = NOW() + ($3::interval),
		       body_text = '',
		       body_html = NULL
		 WHERE id = $1 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, id, newAttempts, fmt.Sprintf("%d seconds", int(backoff.Seconds()))); err != nil {
		return fmt.Errorf("email_outbox: reschedule stranded: %w", err)
	}
	return nil
}

// MarkFailed forces a row to 'failed' immediately (permanent failure path).
// The status='pending' guard means a concurrent MarkSent wins, preserving
// "successful delivery is final". 'failed' is terminal, so the same UPDATE
// scrubs body_text/body_html to avoid keeping a usable recovery token at rest.
func (r *EmailOutboxRepository) MarkFailed(ctx context.Context, id uuid.UUID, lastErr string) error {
	const q = `
		UPDATE email_outbox
		   SET status = 'failed',
		       attempts = attempts + 1,
		       last_error = $2,
		       body_text = '',
		       body_html = NULL
		 WHERE id = $1 AND status = 'pending'`
	if _, err := r.pool.Exec(ctx, q, id, truncateOutboxErr(lastErr)); err != nil {
		return fmt.Errorf("email_outbox: mark failed: %w", err)
	}
	return nil
}

// truncateOutboxErr bounds the last_error column.
func truncateOutboxErr(s string) string {
	if len(s) <= outboxLastErrorMaxLen {
		return s
	}
	return s[:outboxLastErrorMaxLen] + "..."
}
