// Package wire — email.go
//
// transactional email wiring. Lives in this dedicated file
// (NOT databases.go) per : Unisender is stateless HTTP
// with no Close-tracked resource, so it doesn't belong on DBHandles.
//
// Two public constructors:
// - BuildEmailSender(log, cfg) email.Sender — picks UnisenderSender
// when UNISENDER_API_KEY is set, falls back to NoopSender otherwise.
// - StartOutboxWorker(ctx, log, repo, sender, pollInterval, maxAttempts)
// spawns a non-blocking goroutine that polls email_outbox and
// drains pending rows. Lifecycle bound to ctx (SIGTERM cancels).
package wire

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/f1xgun/onevoice/pkg/email"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// outboxBatchLimit caps rows drained per worker tick. Tuned to the v1.4
// beta volume estimate (~10k emails/month ≈ 0.3/min); a single tick
// burst of 25 covers any reasonable spike without starving the DB pool.
const outboxBatchLimit = 25

// BuildEmailSender returns a production Sender when the Unisender API
// key is configured, and a NoopSender otherwise (dev/local). Failing
// open to NoopSender — instead of erroring at startup — matches the
// existing NATS-optional pattern in databases.go: dev environments
// should boot without external dependencies, and the outbox worker
// logs a clear warn line so the operator knows email is no-op.
func BuildEmailSender(log *slog.Logger, cfg *config.Config) email.Sender {
	if cfg.UnisenderAPIKey == "" {
		log.Warn("email: UNISENDER_API_KEY not set — using NoopSender (dev mode). Production MUST set this.")
		return email.NewNoopSender()
	}
	s, err := email.NewUnisenderSender(email.UnisenderConfig{
		APIKey:    cfg.UnisenderAPIKey,
		FromEmail: cfg.UnisenderFromEmail,
		FromName:  cfg.UnisenderFromName,
	})
	if err != nil {
		log.Error("email: NewUnisenderSender failed — falling back to NoopSender", "error", err)
		return email.NewNoopSender()
	}
	log.Info("email: UnisenderSender constructed", "from_email", cfg.UnisenderFromEmail)
	return s
}

// StartOutboxWorker spawns a non-blocking goroutine that polls
// email_outbox every pollInterval. Pattern mirrors StartRetentionSweep
// in wire/policy_sweep.go.
//
// Lifecycle is bound to ctx: SIGTERM cancels the worker. In-flight
// rows stay in 'pending' state (the atomic UPDATE WHERE
// status='pending' guarantees at-least-once delivery on restart per
// T-INF-02).
func StartOutboxWorker(ctx context.Context, log *slog.Logger, repo *repository.EmailOutboxRepository, sender email.Sender, pollInterval time.Duration, maxAttempts int) {
	go runOutboxWorker(ctx, log, repo, sender, pollInterval, maxAttempts)
}

func runOutboxWorker(ctx context.Context, log *slog.Logger, repo *repository.EmailOutboxRepository, sender email.Sender, pollInterval time.Duration, maxAttempts int) {
	log.Info("email_outbox worker: starting", "poll_interval", pollInterval, "max_attempts", maxAttempts)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("email_outbox worker: shutting down")
			return
		case <-ticker.C:
			drainOutboxOnce(ctx, log, repo, sender, maxAttempts)
		}
	}
}

func drainOutboxOnce(ctx context.Context, log *slog.Logger, repo *repository.EmailOutboxRepository, sender email.Sender, maxAttempts int) {
	rows, err := repo.DrainPending(ctx, outboxBatchLimit)
	if err != nil {
		log.ErrorContext(ctx, "email_outbox: drain query failed", "error", err)
		return
	}
	for _, row := range rows {
		jobID, sendErr := sender.Send(ctx, email.Message{
			To:       row.ToEmail,
			Subject:  row.Subject,
			BodyText: row.BodyText,
			BodyHTML: row.BodyHTML,
		})
		if sendErr == nil {
			if mErr := repo.MarkSent(ctx, row.ID, jobID); mErr != nil {
				log.ErrorContext(ctx, "email_outbox: mark_sent failed", "id", row.ID, "error", mErr)
			}
			continue
		}
		if errors.Is(sendErr, email.ErrPermanent) {
			if mErr := repo.MarkFailed(ctx, row.ID, sendErr.Error()); mErr != nil {
				log.ErrorContext(ctx, "email_outbox: mark_failed failed", "id", row.ID, "error", mErr)
			}
			continue
		}
		// Treat any other error (including ErrTransient) as transient → reschedule.
		if rErr := repo.Reschedule(ctx, row.ID, row.Attempts, sendErr.Error(), maxAttempts); rErr != nil {
			log.ErrorContext(ctx, "email_outbox: reschedule failed", "id", row.ID, "error", rErr)
		}
	}
}
