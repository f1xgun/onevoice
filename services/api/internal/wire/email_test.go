package wire

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/email"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// bufLogger returns a slog.Logger writing to buf so tests can assert the
// dev-mode NoopSender warning is loud.
func bufLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestBuildEmailSender(t *testing.T) {
	tests := []struct {
		name        string
		appEnv      string
		apiKey      string
		fromEmail   string
		wantErr     bool
		wantSender  string // "noop" | "unisender"
		wantWarnLog bool
	}{
		{
			name:       "production with key and from configured boots ok",
			appEnv:     "production",
			apiKey:     "real-unisender-key",
			fromEmail:  "noreply@onevoice.app",
			wantSender: "unisender",
		},
		{
			name:      "production with empty key fails closed",
			appEnv:    "production",
			apiKey:    "",
			fromEmail: "noreply@onevoice.app",
			wantErr:   true,
		},
		{
			name:      "production with empty from fails closed",
			appEnv:    "production",
			apiKey:    "real-unisender-key",
			fromEmail: "",
			wantErr:   true,
		},
		{
			name:        "dev with empty key falls back to noop with warning",
			appEnv:      "",
			apiKey:      "",
			fromEmail:   "noreply@onevoice.app",
			wantSender:  "noop",
			wantWarnLog: true,
		},
		{
			name:       "staging (non-prod) with key uses unisender",
			appEnv:     "staging",
			apiKey:     "real-unisender-key",
			fromEmail:  "noreply@onevoice.app",
			wantSender: "unisender",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			cfg := &config.Config{
				AppEnv:             tt.appEnv,
				UnisenderAPIKey:    tt.apiKey,
				UnisenderFromEmail: tt.fromEmail,
				UnisenderFromName:  "OneVoice",
			}

			sender, err := BuildEmailSender(bufLogger(buf), cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected fail-closed error, got nil (sender=%v)", sender)
				}
				if sender != nil {
					t.Fatalf("expected nil sender on error, got %v", sender)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			switch tt.wantSender {
			case "noop":
				if _, ok := sender.(*email.NoopSender); !ok {
					t.Fatalf("expected *email.NoopSender, got %T", sender)
				}
			case "unisender":
				if _, ok := sender.(*email.NoopSender); ok {
					t.Fatalf("expected a real sender, got NoopSender")
				}
			}
			if tt.wantWarnLog && !strings.Contains(buf.String(), "DROPPED") {
				t.Fatalf("expected loud DROPPED warning in dev mode, log was: %q", buf.String())
			}
		})
	}
}

func TestSendResult(t *testing.T) {
	tests := []struct {
		name   string
		sender email.Sender
		want   string
	}{
		{name: "noop sender drops", sender: email.NewNoopSender(), want: "noop_dropped"},
		{name: "real sender sends", sender: stubSender{}, want: "sent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sendResult(tt.sender); got != tt.want {
				t.Fatalf("sendResult = %q, want %q", got, tt.want)
			}
		})
	}
}

// stubSender is a non-Noop Sender used to assert sendResult returns "sent" for
// any real transport.
type stubSender struct{}

func (stubSender) Send(_ context.Context, _ email.Message) (string, error) { return "job-1", nil }

// errSender returns the configured error on every Send so drainOutboxOnce
// exercises the reschedule / dead-letter metric paths.
type errSender struct{ err error }

func (s errSender) Send(_ context.Context, _ email.Message) (string, error) { return "", s.err }

// expectPendingCount queues the per-tick CountPending sample drainOutboxOnce
// emits before draining.
func expectPendingCount(mock pgxmock.PgxPoolIface, n int) {
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM email_outbox WHERE status = 'pending'`).
		WillReturnRows(mock.NewRows([]string{"count"}).AddRow(n))
}

// drainRow seeds a single due pending row for DrainPending to return.
func drainRow(mock pgxmock.PgxPoolIface, id uuid.UUID, attempts int) {
	mock.ExpectQuery(`SELECT .+ FROM email_outbox WHERE status = 'pending'`).
		WithArgs(outboxBatchLimit).
		WillReturnRows(mock.NewRows([]string{"id", "to_email", "subject", "body_text", "body_html", "attempts", "created_at"}).
			AddRow(id, "u@example.com", "subj", "body", "", attempts, time.Now().UTC()))
}

func TestDrainOutboxOnce_Metrics(t *testing.T) {
	t.Run("noop send records noop_dropped and pending gauge", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		t.Cleanup(mock.Close)
		repo := repository.NewEmailOutboxRepository(mock)
		id := uuid.New()

		expectPendingCount(mock, 3)
		drainRow(mock, id, 0)
		mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'sent'`).
			WithArgs(id).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		before := testutil.ToFloat64(metrics.EmailsSentTotal.WithLabelValues("noop_dropped"))
		drainOutboxOnce(context.Background(), bufLogger(&bytes.Buffer{}), repo, email.NewNoopSender(), 5)
		after := testutil.ToFloat64(metrics.EmailsSentTotal.WithLabelValues("noop_dropped"))

		require.Equal(t, 1.0, after-before)
		require.Equal(t, 3.0, testutil.ToFloat64(metrics.OutboxPendingRows))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transient error below cap records reschedule", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		t.Cleanup(mock.Close)
		repo := repository.NewEmailOutboxRepository(mock)
		id := uuid.New()

		expectPendingCount(mock, 1)
		drainRow(mock, id, 0)
		mock.ExpectExec(`UPDATE email_outbox\s+SET attempts = \$2`).
			WithArgs(id, 1, fmt.Sprintf("%v", email.ErrTransient), "120 seconds").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		before := testutil.ToFloat64(metrics.EmailsRescheduledTotal)
		drainOutboxOnce(context.Background(), bufLogger(&bytes.Buffer{}), repo, errSender{err: email.ErrTransient}, 5)
		after := testutil.ToFloat64(metrics.EmailsRescheduledTotal)

		require.Equal(t, 1.0, after-before)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("permanent error records dead-letter", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		t.Cleanup(mock.Close)
		repo := repository.NewEmailOutboxRepository(mock)
		id := uuid.New()

		expectPendingCount(mock, 1)
		drainRow(mock, id, 0)
		mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'failed'`).
			WithArgs(id, fmt.Sprintf("%v", email.ErrPermanent)).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		before := testutil.ToFloat64(metrics.EmailsDeadLetteredTotal)
		drainOutboxOnce(context.Background(), bufLogger(&bytes.Buffer{}), repo, errSender{err: email.ErrPermanent}, 5)
		after := testutil.ToFloat64(metrics.EmailsDeadLetteredTotal)

		require.Equal(t, 1.0, after-before)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transient error at cap records dead-letter", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		t.Cleanup(mock.Close)
		repo := repository.NewEmailOutboxRepository(mock)
		id := uuid.New()

		expectPendingCount(mock, 1)
		drainRow(mock, id, 4)
		mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'failed',\s+attempts = \$2`).
			WithArgs(id, 5, fmt.Sprintf("%v", email.ErrTransient)).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		before := testutil.ToFloat64(metrics.EmailsDeadLetteredTotal)
		drainOutboxOnce(context.Background(), bufLogger(&bytes.Buffer{}), repo, errSender{err: email.ErrTransient}, 5)
		after := testutil.ToFloat64(metrics.EmailsDeadLetteredTotal)

		require.Equal(t, 1.0, after-before)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

// syncBuffer is a bytes.Buffer guarded by a mutex so the worker goroutine and
// the test can write/read the slog output without a data race under -race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestStartOutboxWorker_JoinedByWaitGroup proves StartOutboxWorker registers
// its goroutine on the WaitGroup so the shutdown sequence can join it before
// the pools close. The worker is given a long poll interval (it never ticks),
// the ctx is canceled, and the WaitGroup is awaited. When the join returns, the
// worker must have actually returned — evidenced by its "shutting down" log
// line being present.
//
// Fail-on-revert: drop the wg.Add/wg.Done tracking in StartOutboxWorker (back
// to a bare `go runOutboxWorker(...)`) and wg.Wait() returns before the
// goroutine has logged its shutdown — the assertion below fails because the
// log buffer is still empty at the moment the (untracked) Wait returns.
func TestStartOutboxWorker_JoinedByWaitGroup(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	repo := repository.NewEmailOutboxRepository(mock)

	sb := &syncBuffer{}
	log := slog.New(slog.NewTextHandler(sb, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	StartOutboxWorker(ctx, &wg, log, repo, email.NewNoopSender(), time.Hour, 5)

	cancel()

	joined := make(chan struct{})
	go func() {
		wg.Wait()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait() did not return: StartOutboxWorker goroutine was not joined")
	}

	require.Contains(t, sb.String(), "shutting down",
		"WaitGroup returned before the outbox worker actually exited — the worker is not tracked on the WaitGroup, so shutdown can close the pool while it is mid-pass")
}

// ctxCapturePool wraps a pgxmock pool to record the context handed to the
// post-delivery Exec (the MarkSent UPDATE), letting the SIGTERM regression
// test assert that persist runs on a non-canceled context.
type ctxCapturePool struct {
	pgxmock.PgxPoolIface
	mu         sync.Mutex
	execCalled bool
	execCtxErr error
}

func (p *ctxCapturePool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	p.mu.Lock()
	p.execCalled = true
	p.execCtxErr = ctx.Err()
	p.mu.Unlock()
	return p.PgxPoolIface.Exec(ctx, sql, args...)
}

func (p *ctxCapturePool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.PgxPoolIface.Query(ctx, sql, args...)
}

func (p *ctxCapturePool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.PgxPoolIface.QueryRow(ctx, sql, args...)
}

// execObserved reports whether Exec ran and the ctx.Err() captured at the
// moment of the call (nil = the persist ctx was live, not canceled).
func (p *ctxCapturePool) execObserved() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.execCalled, p.execCtxErr
}

// cancelOnSendSender models a SIGTERM landing in the window between a
// successful delivery and its persist: it reports success, then cancels the
// worker (parent) ctx before returning, so MarkSent runs under an
// already-canceled parent.
type cancelOnSendSender struct{ cancel context.CancelFunc }

func (s cancelOnSendSender) Send(_ context.Context, _ email.Message) (string, error) {
	s.cancel()
	return "job-1", nil
}

// TestDrainOutboxOnce_MarkSentSurvivesCanceledParent proves the dup-email
// guard: a SIGTERM that cancels the worker ctx right after a successful Send
// must NOT abort the 'sent' persist, or the row stays 'pending' and the next
// tick re-Sends it (Unisender has no idempotency key → duplicate mail).
//
// The fake sender cancels the parent ctx the moment delivery succeeds, exactly
// the SIGTERM-after-Send window cmd/main.go exposes. The success-path UPDATE
// must still execute on a non-canceled context.
//
// Fail-on-revert: route MarkSent through the parent ctx instead of
// context.Background() and the UPDATE never runs (canceled ctx) — both the
// ExpectationsWereMet and the captured-ctx Err()==nil assertions fail.
func TestDrainOutboxOnce_MarkSentSurvivesCanceledParent(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	pool := &ctxCapturePool{PgxPoolIface: mock}
	repo := repository.NewEmailOutboxRepository(pool)
	id := uuid.New()

	expectPendingCount(mock, 1)
	drainRow(mock, id, 0)
	mock.ExpectExec(`UPDATE email_outbox\s+SET status = 'sent'`).
		WithArgs(id).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	drainOutboxOnce(parent, bufLogger(&bytes.Buffer{}), repo, cancelOnSendSender{cancel: cancel}, 5)

	require.Error(t, parent.Err(), "precondition: the SIGTERM-after-Send window must have canceled the parent ctx")
	require.NoError(t, mock.ExpectationsWereMet(), "MarkSent UPDATE must run even though the parent ctx was canceled")
	called, ctxErr := pool.execObserved()
	require.True(t, called, "MarkSent must have been invoked")
	require.NoError(t, ctxErr, "MarkSent must run on a non-canceled context so a just-sent row is persisted")
}
