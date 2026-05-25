package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

// newEmailOutboxRepoMock returns a fresh pgxmock pool + EmailOutbox
// repository. Mirrors newInvitationRepoMock so all repo tests share the
// same pgxmock shape.
func newEmailOutboxRepoMock(t *testing.T) (pgxmock.PgxPoolIface, *EmailOutboxRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewEmailOutboxRepository(mock)
}

// TestEmailOutbox_TableExists verifies the repository constructor and
// pgxPool wiring without exercising any SQL. The actual schema-presence
// check is the responsibility of the integration migration run; this
// unit test is the smoke gate that NewEmailOutboxRepository returns a
// non-nil repo against a working mock pool.
func TestEmailOutbox_TableExists(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	require.NotNil(t, repo)
	require.NotNil(t, mock)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_Enqueue_Commit(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	expectedID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO email_outbox`).
		WithArgs("alice@example.com", "Reset", "Plain body", "<p>HTML</p>").
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(expectedID))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	got, err := repo.Enqueue(ctx, tx, OutboxEnqueueInput{
		ToEmail:  "alice@example.com",
		Subject:  "Reset",
		BodyText: "Plain body",
		BodyHTML: "<p>HTML</p>",
	})
	require.NoError(t, err)
	require.Equal(t, expectedID, got)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_Enqueue_Rollback is the load-bearing atomicity test
// (T-INF-01 in the threat model). When the caller's tx rolls back, the
// outbox row vanishes — pgxmock proves this by NOT recording a
// post-rollback row state, but the contract we assert is that
// Enqueue's INSERT happens INSIDE the tx so rollback is sufficient.
func TestEmailOutbox_Enqueue_Rollback(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	expectedID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO email_outbox`).
		WithArgs("alice@example.com", "Reset", "Plain body", "").
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(expectedID))
	mock.ExpectRollback()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	_, err = repo.Enqueue(ctx, tx, OutboxEnqueueInput{
		ToEmail:  "alice@example.com",
		Subject:  "Reset",
		BodyText: "Plain body",
		// BodyHTML omitted = ""
	})
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	// Critical assertion: every expected interaction was through tx
	// (Begin + INSERT + Rollback). The mock would error if any SQL
	// happened against the underlying pool outside the tx.
	require.NoError(t, mock.ExpectationsWereMet())
}

// Phase 21-04 (21-CROSS-PLAN-CONTRACTS §3): Enqueue now ACCEPTS nil tx
// and falls back to a pool INSERT so sweeper-driven sends (T-7 deletion
// warning) can enqueue without a surrounding business tx. This test
// previously asserted rejection — it now asserts the fallback path
// executes a single INSERT via the pool.
func TestEmailOutbox_Enqueue_NilTxFallsBackToPool(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	expectedID := uuid.New()

	// No ExpectBegin/Commit — the pool path issues a bare QueryRow.
	mock.ExpectQuery(`INSERT INTO email_outbox`).
		WithArgs("sweeper@example.com", "Удаление аккаунта — осталось 7 дней", "txt", "html").
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(expectedID))

	got, err := repo.Enqueue(ctx, nil, OutboxEnqueueInput{
		ToEmail:  "sweeper@example.com",
		Subject:  "Удаление аккаунта — осталось 7 дней",
		BodyText: "txt",
		BodyHTML: "html",
	})
	require.NoError(t, err)
	require.Equal(t, expectedID, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_EnqueueDeferred_Tx — Phase 21-04 §2a. The deferred
// variant writes an explicit next_attempt_at so the worker won't pick
// the row up until then (used by the T-7 deletion warning enqueue at
// request-deletion time, scheduled +23 days).
func TestEmailOutbox_EnqueueDeferred_Tx(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	expectedID := uuid.New()
	nextAttempt := time.Now().Add(23 * 24 * time.Hour).UTC().Truncate(time.Second)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO email_outbox \(to_email, subject, body_text, body_html, next_attempt_at\)`).
		WithArgs("user@example.com", "Удаление аккаунта — осталось 7 дней", "txt", "html", nextAttempt).
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(expectedID))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	got, err := repo.EnqueueDeferred(ctx, tx, OutboxEnqueueInput{
		ToEmail:  "user@example.com",
		Subject:  "Удаление аккаунта — осталось 7 дней",
		BodyText: "txt",
		BodyHTML: "html",
	}, nextAttempt)
	require.NoError(t, err)
	require.Equal(t, expectedID, got)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_ExistsBySubjectAndRecipient — Phase 21-04 §2b. Returns
// true if a row exists for (to_email, subject) in ANY status. Used by
// the warning sweeper to dedupe.
func TestEmailOutbox_ExistsBySubjectAndRecipient(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT EXISTS \(\s*SELECT 1 FROM email_outbox WHERE to_email = \$1 AND subject = \$2\s*\)`).
		WithArgs("user@example.com", "Удаление аккаунта — осталось 7 дней").
		WillReturnRows(mock.NewRows([]string{"exists"}).AddRow(true))

	exists, err := repo.ExistsBySubjectAndRecipient(ctx, "user@example.com", "Удаление аккаунта — осталось 7 дней")
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_ExistsBySubjectAndRecipient_FalseWhenAbsent confirms the
// query returns false when no row matches (the warning sweeper's primary
// path on the first run).
func TestEmailOutbox_ExistsBySubjectAndRecipient_FalseWhenAbsent(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs("never@example.com", "subj").
		WillReturnRows(mock.NewRows([]string{"exists"}).AddRow(false))

	exists, err := repo.ExistsBySubjectAndRecipient(ctx, "never@example.com", "subj")
	require.NoError(t, err)
	require.False(t, exists)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_CancelPendingBySubjectAndRecipient — Phase 21-04. On
// POST /users/me/restore, the pending T-7 warning row should be
// canceled so the user doesn't receive the warning after restoring.
func TestEmailOutbox_CancelPendingBySubjectAndRecipient(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'canceled'.*WHERE to_email = \$1 AND subject = \$2 AND status = 'pending'`).
		WithArgs("user@example.com", "Удаление аккаунта — осталось 7 дней").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.CancelPendingBySubjectAndRecipient(ctx, "user@example.com", "Удаление аккаунта — осталось 7 дней"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_DrainPending_ReturnsDuePendingOnly(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	dueID := uuid.New()
	dueTime := time.Now().UTC().Add(-1 * time.Minute)

	// Mock returns the single due+pending row. The query's WHERE clause
	// is the load-bearing assertion: it filters status='pending' AND
	// next_attempt_at <= NOW() server-side, and orders ASC. Pgxmock
	// regex-matches the SQL and replays the rows we hand it.
	mock.ExpectQuery(`SELECT id, to_email, subject, body_text, COALESCE\(body_html, ''\), attempts, created_at\s+FROM email_outbox\s+WHERE status = 'pending'\s+AND next_attempt_at <= NOW\(\)\s+ORDER BY next_attempt_at ASC\s+LIMIT \$1`).
		WithArgs(10).
		WillReturnRows(mock.NewRows([]string{"id", "to_email", "subject", "body_text", "body_html", "attempts", "created_at"}).
			AddRow(dueID, "due@example.com", "subj", "text", "", 0, dueTime))

	rows, err := repo.DrainPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, dueID, rows[0].ID)
	require.Equal(t, "due@example.com", rows[0].ToEmail)
	require.Equal(t, 0, rows[0].Attempts)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_DrainPending_LimitRespected(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	// Build 5 rows even though "12 pending due" would exist in a real
	// DB — the SQL's LIMIT $1 enforces the cap server-side, and the
	// mock proves the limit bind value is correctly threaded.
	rs := mock.NewRows([]string{"id", "to_email", "subject", "body_text", "body_html", "attempts", "created_at"})
	for i := 0; i < 5; i++ {
		rs.AddRow(uuid.New(), fmt.Sprintf("u%d@example.com", i), "subj", "text", "", 0, time.Now().UTC())
	}
	mock.ExpectQuery(`SELECT .+ FROM email_outbox WHERE status = 'pending'.+LIMIT \$1`).
		WithArgs(5).
		WillReturnRows(rs)

	rows, err := repo.DrainPending(ctx, 5)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_MarkSent(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	// MarkSent SQL atomically transitions to 'sent' guarded by status='pending'.
	// last_error is cleared. attempts is incremented by 1.
	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'sent',\s+sent_at = NOW\(\),\s+last_error = NULL,\s+attempts = attempts \+ 1\s+WHERE id = \$1 AND status = 'pending'`).
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.MarkSent(ctx, id, "job-abc"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_MarkSent_Idempotent verifies the restart-safe
// invariant T-INF-02: a second MarkSent against an already-sent row
// affects 0 rows but does NOT return an error.
func TestEmailOutbox_MarkSent_Idempotent(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	mock.ExpectExec(`UPDATE email_outbox`).
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	require.NoError(t, repo.MarkSent(ctx, id, "ignored-job-id"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_Reschedule_ExpBackoff verifies that newAttempts=3
// (currentAttempts=2 + 1) results in a `8 seconds-formatted backoff
// interval of 480 seconds (8 minutes) being passed as the bind value.
// The DB-side NOW() + interval addition is opaque to the mock; the
// load-bearing assertion is the formatted "480 seconds" interval bind.
func TestEmailOutbox_Reschedule_ExpBackoff(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	// currentAttempts=2 + 1 = newAttempts=3 → 2^3 minutes = 480 seconds.
	mock.ExpectExec(`UPDATE email_outbox\s+SET attempts = \$2,\s+last_error = \$3,\s+next_attempt_at = NOW\(\) \+ \(\$4::interval\)\s+WHERE id = \$1 AND status = 'pending'`).
		WithArgs(id, 3, "transient err", "480 seconds").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.Reschedule(ctx, id, 2, "transient err", 5))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_Reschedule_AtCapMarksFailed(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	// currentAttempts=4 + 1 = newAttempts=5; newAttempts >= maxAttempts (5)
	// → transitions to status='failed' branch.
	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'failed',\s+attempts = \$2,\s+last_error = \$3\s+WHERE id = \$1 AND status = 'pending'`).
		WithArgs(id, 5, "final fail").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.Reschedule(ctx, id, 4, "final fail", 5))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_MarkFailed(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'failed',\s+attempts = attempts \+ 1,\s+last_error = \$2\s+WHERE id = \$1 AND status = 'pending'`).
		WithArgs(id, "permanent error").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.MarkFailed(ctx, id, "permanent error"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// Smoke-tests for error-truncation guard. Long last_error strings are
// trimmed to outboxLastErrorMaxLen + "..." so a pathological Unisender
// response can't bloat the table (T-INF-07).
func TestEmailOutbox_Reschedule_TruncatesLongError(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	huge := strings.Repeat("X", outboxLastErrorMaxLen+500)
	expectedTrimmed := strings.Repeat("X", outboxLastErrorMaxLen) + "..."

	// currentAttempts=0 + 1 = newAttempts=1 → 2^1 minutes = 120 seconds.
	mock.ExpectExec(`UPDATE email_outbox`).
		WithArgs(id, 1, expectedTrimmed, "120 seconds").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.Reschedule(ctx, id, 0, huge, 5))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_Enqueue_QueryError(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO email_outbox`).
		WithArgs("a@b.c", "s", "t", "").
		WillReturnError(pgx.ErrTxClosed)
	mock.ExpectRollback()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	_, err = repo.Enqueue(ctx, tx, OutboxEnqueueInput{ToEmail: "a@b.c", Subject: "s", BodyText: "t"})
	require.Error(t, err)
	require.True(t, errors.Is(err, pgx.ErrTxClosed) || strings.Contains(err.Error(), "enqueue"))
	require.NoError(t, tx.Rollback(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}
