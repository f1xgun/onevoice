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
	"github.com/jackc/pgx/v5/pgconn"
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
		WithArgs("alice@example.com", "Reset", "Plain body", "<p>HTML</p>", (*uuid.UUID)(nil)).
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
		WithArgs("alice@example.com", "Reset", "Plain body", "", (*uuid.UUID)(nil)).
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(expectedID))
	mock.ExpectRollback()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	_, err = repo.Enqueue(ctx, tx, OutboxEnqueueInput{
		ToEmail:  "alice@example.com",
		Subject:  "Reset",
		BodyText: "Plain body",
	})
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// Enqueue accepts nil tx and falls back to a pool INSERT so sweeper-driven
// sends can enqueue without a surrounding business tx; assert the fallback
// path executes a single INSERT via the pool.
func TestEmailOutbox_Enqueue_NilTxFallsBackToPool(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	expectedID := uuid.New()

	mock.ExpectQuery(`INSERT INTO email_outbox`).
		WithArgs("sweeper@example.com", "Удаление аккаунта — осталось 7 дней", "txt", "html", (*uuid.UUID)(nil)).
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

// TestEmailOutbox_EnqueueDeferred_Tx — §2a. The deferred
// variant writes an explicit next_attempt_at so the worker won't pick
// the row up until then (used by the T-7 deletion warning enqueue at
// request-deletion time, scheduled +23 days).
func TestEmailOutbox_EnqueueDeferred_Tx(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	expectedID := uuid.New()
	nextAttempt := time.Now().Add(23 * 24 * time.Hour).UTC().Truncate(time.Second)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO email_outbox \(to_email, subject, body_text, body_html, next_attempt_at, business_id\)`).
		WithArgs("user@example.com", "Удаление аккаунта — осталось 7 дней", "txt", "html", nextAttempt, (*uuid.UUID)(nil)).
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

// TestEmailOutbox_EnqueueDeferred_WritesBusinessID verifies the organization
// T-7 path persists its business_id so the per-organization cancel can later
// target exactly this row. A NULL business_id here would leave the cancel
// unable to disambiguate sibling organizations.
func TestEmailOutbox_EnqueueDeferred_WritesBusinessID(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	expectedID := uuid.New()
	businessID := uuid.New()
	nextAttempt := time.Now().Add(23 * 24 * time.Hour).UTC().Truncate(time.Second)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO email_outbox \(to_email, subject, body_text, body_html, next_attempt_at, business_id\)`).
		WithArgs("owner@example.com", "Удаление организации — осталось 7 дней", "txt", "html", nextAttempt, &businessID).
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(expectedID))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	got, err := repo.EnqueueDeferred(ctx, tx, OutboxEnqueueInput{
		ToEmail:    "owner@example.com",
		Subject:    "Удаление организации — осталось 7 дней",
		BodyText:   "txt",
		BodyHTML:   "html",
		BusinessID: &businessID,
	}, nextAttempt)
	require.NoError(t, err)
	require.Equal(t, expectedID, got)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_ExistsBySubjectAndRecipient — §2b. Returns
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

// TestEmailOutbox_CancelPendingBySubjectAndRecipient —. On
// POST /users/me/restore, the pending T-7 warning row should be
// canceled so the user doesn't receive the warning after restoring.
func TestEmailOutbox_CancelPendingBySubjectAndRecipient(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'canceled',\s+last_error = 'canceled: deletion restored',\s+body_text = '',\s+body_html = NULL\s+WHERE to_email = \$1 AND subject = \$2 AND status = 'pending'`).
		WithArgs("user@example.com", "Удаление аккаунта — осталось 7 дней").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.CancelPendingBySubjectAndRecipient(ctx, "user@example.com", "Удаление аккаунта — осталось 7 дней"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_CancelPendingBusinessT7_ScopesToBusiness is the fail-on-revert
// guard for the sibling-overcancel bug. An owner with two pending organization
// deletions has two pending T-7 rows with identical (to_email, subject) but
// distinct business_id. Restoring organization A must cancel ONLY A's row. The
// cancel UPDATE must therefore carry a `business_id = $3` predicate bound to A's
// id; reverting to the unscoped (to_email, subject)-only UPDATE drops that
// predicate and bind arg, and these assertions fail.
func TestEmailOutbox_CancelPendingBusinessT7_ScopesToBusiness(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	businessA := uuid.New()
	businessB := uuid.New()
	const subject = "Удаление организации — осталось 7 дней"

	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'canceled',\s+last_error = 'canceled: deletion restored',\s+body_text = '',\s+body_html = NULL\s+WHERE to_email = \$1 AND subject = \$2 AND business_id = \$3 AND status = 'pending'`).
		WithArgs("owner@example.com", subject, businessA).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	capture := &sqlCapturePool{pgxPool: mock}
	repo := NewEmailOutboxRepository(capture)

	require.NoError(t, repo.CancelPendingBusinessT7ByRecipient(context.Background(), "owner@example.com", subject, businessA))
	require.NoError(t, mock.ExpectationsWereMet())

	require.Contains(t, capture.execSQL, "business_id = $3",
		"cancel must scope to one organization's business_id so siblings survive")
	require.NotContains(t, capture.execSQL, businessB.String(),
		"the sibling organization's id must never appear in the cancel statement")
}

func TestEmailOutbox_DrainPending_ReturnsDuePendingOnly(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	dueID := uuid.New()
	dueTime := time.Now().UTC().Add(-1 * time.Minute)

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

func TestEmailOutbox_CountPending(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM email_outbox WHERE status = 'pending'`).
		WillReturnRows(mock.NewRows([]string{"count"}).AddRow(7))

	n, err := repo.CountPending(ctx)
	require.NoError(t, err)
	require.Equal(t, 7, n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_CountPending_QueryError(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM email_outbox`).
		WillReturnError(pgx.ErrTxClosed)

	_, err := repo.CountPending(ctx)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_MarkSent(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'sent',\s+sent_at = NOW\(\),\s+last_error = NULL,\s+attempts = attempts \+ 1,\s+body_text = '',\s+body_html = NULL\s+WHERE id = \$1 AND status = 'pending'`).
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
// The DB-side NOW + interval addition is opaque to the mock; the
// load-bearing assertion is the formatted "480 seconds" interval bind.
func TestEmailOutbox_Reschedule_ExpBackoff(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

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

	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'failed',\s+attempts = \$2,\s+last_error = \$3,\s+body_text = '',\s+body_html = NULL\s+WHERE id = \$1 AND status = 'pending'`).
		WithArgs(id, 5, "final fail").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.Reschedule(ctx, id, 4, "final fail", 5))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailOutbox_MarkFailed(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'failed',\s+attempts = attempts \+ 1,\s+last_error = \$2,\s+body_text = '',\s+body_html = NULL\s+WHERE id = \$1 AND status = 'pending'`).
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
		WithArgs("a@b.c", "s", "t", "", (*uuid.UUID)(nil)).
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

// sqlCapturePool records the SQL of the first Exec so the body-scrub tests can
// assert the terminal UPDATE clears body_text/body_html (rather than only
// matching a pre-baked regex). It delegates everything else to the wrapped
// pgxmock pool.
type sqlCapturePool struct {
	pgxPool
	execSQL string
}

func (p *sqlCapturePool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if p.execSQL == "" {
		p.execSQL = sql
	}
	return p.pgxPool.Exec(ctx, sql, args...)
}

// terminalScrubTokenBody is a stand-in for a password-reset / verify email body
// carrying the LIVE plaintext recovery token in its reset link.
const terminalScrubTokenBody = "Reset your password: https://app.example.com/reset?token=LIVE_PLAINTEXT_TOKEN_abc123"

// TestEmailOutbox_TerminalTransitionsScrubBody is the fail-on-revert guard for
// the secret-at-rest fix: every terminal transition (sent / failed via
// MarkFailed / failed via Reschedule-at-cap / canceled) must clear body_text
// and body_html in the SAME UPDATE so a delivered/terminated row stops holding
// a usable recovery link. Reverting the scrub leaves the SET clause without the
// body columns and every sub-test's assertions fail.
//
// We assert the emitted SQL clears body_text and sets body_html to NULL. The
// body itself is never a bind arg on these by-id UPDATEs, so a row that started
// with terminalScrubTokenBody can no longer yield a working token once the
// UPDATE runs.
func TestEmailOutbox_TerminalTransitionsScrubBody(t *testing.T) {
	require.Contains(t, terminalScrubTokenBody, "token=", "precondition: the fixture body must embed a recovery token")

	id := uuid.New()
	cases := []struct {
		name string
		run  func(t *testing.T, repo *EmailOutboxRepository)
		mock func(mock pgxmock.PgxPoolIface)
	}{
		{
			name: "sent",
			mock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE email_outbox`).WithArgs(id).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			run: func(t *testing.T, repo *EmailOutboxRepository) {
				require.NoError(t, repo.MarkSent(context.Background(), id, "job-1"))
			},
		},
		{
			name: "failed_permanent",
			mock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE email_outbox`).WithArgs(id, "permanent").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			run: func(t *testing.T, repo *EmailOutboxRepository) {
				require.NoError(t, repo.MarkFailed(context.Background(), id, "permanent"))
			},
		},
		{
			name: "failed_at_cap",
			mock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE email_outbox`).WithArgs(id, 5, "final").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			run: func(t *testing.T, repo *EmailOutboxRepository) {
				require.NoError(t, repo.Reschedule(context.Background(), id, 4, "final", 5))
			},
		},
		{
			name: "canceled",
			mock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE email_outbox`).WithArgs("u@example.com", "subj").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			run: func(t *testing.T, repo *EmailOutboxRepository) {
				require.NoError(t, repo.CancelPendingBySubjectAndRecipient(context.Background(), "u@example.com", "subj"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			t.Cleanup(mock.Close)
			tc.mock(mock)

			capture := &sqlCapturePool{pgxPool: mock}
			repo := NewEmailOutboxRepository(capture)

			tc.run(t, repo)

			require.NoError(t, mock.ExpectationsWereMet())
			require.Contains(t, capture.execSQL, "body_text = ''", "terminal UPDATE must scrub body_text")
			require.Contains(t, capture.execSQL, "body_html = NULL", "terminal UPDATE must scrub body_html")
		})
	}
}

// TestEmailOutbox_Reschedule_RetryKeepsBody asserts the NON-terminal retry path
// leaves the body intact so a later attempt can still deliver the email. This
// pins the boundary of the scrub: only terminal states clear the body.
func TestEmailOutbox_Reschedule_RetryKeepsBody(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	id := uuid.New()

	mock.ExpectExec(`UPDATE email_outbox`).
		WithArgs(id, 1, "transient", "120 seconds").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	capture := &sqlCapturePool{pgxPool: mock}
	repo := NewEmailOutboxRepository(capture)

	require.NoError(t, repo.Reschedule(context.Background(), id, 0, "transient", 5))
	require.NoError(t, mock.ExpectationsWereMet())
	require.NotContains(t, capture.execSQL, "body_text = ''", "retry path must NOT scrub the body — a later attempt re-reads it")
	require.NotContains(t, capture.execSQL, "body_html = NULL", "retry path must NOT scrub the body — a later attempt re-reads it")
}

// TestEmailOutbox_RescheduleStrandedSent_BacksOffAndScrubs verifies the
// stranded-sent path (Send succeeded, MarkSent failed): the non-terminal branch
// must bump next_attempt_at by exp-backoff, count the attempt, AND scrub the
// body — unlike plain Reschedule, which keeps the body for a later re-send.
// Delivery already happened, so retaining the body would leave a live recovery
// token at rest.
func TestEmailOutbox_RescheduleStrandedSent_BacksOffAndScrubs(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	mock.ExpectExec(`UPDATE email_outbox\s+SET attempts = \$2,\s+last_error = 'stranded after successful delivery',\s+next_attempt_at = NOW\(\) \+ \(\$3::interval\),\s+body_text = '',\s+body_html = NULL\s+WHERE id = \$1 AND status = 'pending'`).
		WithArgs(id, 3, "480 seconds").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.RescheduleStrandedSent(ctx, id, 2, 5))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailOutbox_RescheduleStrandedSent_AtCapMarksFailed verifies the stranded
// row eventually dead-letters: at maxAttempts it transitions to 'failed' (so it
// stops being re-drained) rather than re-sending forever.
func TestEmailOutbox_RescheduleStrandedSent_AtCapMarksFailed(t *testing.T) {
	mock, repo := newEmailOutboxRepoMock(t)
	ctx := context.Background()
	id := uuid.New()

	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'failed',\s+attempts = \$2,\s+last_error = 'stranded after successful delivery',\s+body_text = '',\s+body_html = NULL\s+WHERE id = \$1 AND status = 'pending'`).
		WithArgs(id, 5).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.RescheduleStrandedSent(ctx, id, 4, 5))
	require.NoError(t, mock.ExpectationsWereMet())
}
