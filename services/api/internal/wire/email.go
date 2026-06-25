// Package wire — email.go
//
// transactional email wiring. Lives in this dedicated file
// (NOT databases.go) because Unisender is stateless HTTP
// with no Close-tracked resource, so it doesn't belong on DBHandles.
//
// Two public constructors:
// - BuildEmailSender(log, cfg) (email.Sender, error) — picks UnisenderSender
// when UNISENDER_API_KEY (and a From address) are set; in production a missing
// configuration is a hard boot error (mail must not be silently dropped), while
// dev/non-prod falls back to NoopSender with a loud warning.
// - StartOutboxWorker(ctx, log, repo, sender, pollInterval, maxAttempts)
// spawns a non-blocking goroutine that polls email_outbox and
// drains pending rows. Lifecycle bound to ctx (SIGTERM cancels).
package wire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/f1xgun/onevoice/pkg/email"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// outboxBatchLimit caps rows drained per worker tick. Tuned to the v1.4
// beta volume estimate (~10k emails/month ≈ 0.3/min); a single tick
// burst of 25 covers any reasonable spike without starving the DB pool.
const outboxBatchLimit = 25

// outboxPersistTimeout bounds the post-delivery status UPDATE. The transition
// runs on a non-cancelable context (context.Background) so a SIGTERM that
// cancels the worker ctx in the window between a successful Send and its
// persist can't strand a just-delivered row in 'pending' — the next tick (or
// next boot) would otherwise re-Send it and the recipient gets a duplicate,
// as Unisender has no idempotency key.
const outboxPersistTimeout = 5 * time.Second

// BuildEmailSender returns the transactional-email Sender.
//
// In production (cfg.IsProduction) a missing UNISENDER_API_KEY or From
// address is a hard boot error: NoopSender silently accepts every message and
// the worker would then mark the row 'sent', so verify/reset/invite mail would
// be dropped while onboarding and account-recovery flows believe it was
// delivered. Failing the boot mirrors the JWT_SECRET / ENCRYPTION_KEY
// deny-list gates — a misconfigured production deploy must not start.
//
// In dev/non-prod a missing key falls back to NoopSender with a loud warning
// so local development boots without an external dependency.
func BuildEmailSender(log *slog.Logger, cfg *config.Config) (email.Sender, error) {
	if cfg.UnisenderAPIKey == "" || cfg.UnisenderFromEmail == "" {
		if cfg.IsProduction() {
			return nil, fmt.Errorf("email: UNISENDER_API_KEY and UNISENDER_FROM_EMAIL are required in production (transactional mail must not be silently dropped)")
		}
		log.Warn("email: UNISENDER_API_KEY/UNISENDER_FROM_EMAIL not set — using NoopSender (dev mode). Mail will be DROPPED, not delivered. Production MUST set these.")
		return email.NewNoopSender(), nil
	}
	s, err := email.NewUnisenderSender(email.UnisenderConfig{
		APIKey:    cfg.UnisenderAPIKey,
		FromEmail: cfg.UnisenderFromEmail,
		FromName:  cfg.UnisenderFromName,
	})
	if err != nil {
		if cfg.IsProduction() {
			return nil, fmt.Errorf("email: NewUnisenderSender failed in production: %w", err)
		}
		log.Error("email: NewUnisenderSender failed — falling back to NoopSender (dev mode)", "error", err)
		return email.NewNoopSender(), nil
	}
	log.Info("email: UnisenderSender constructed", "from_email", cfg.UnisenderFromEmail)
	return s, nil
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
	if pending, err := repo.CountPending(ctx); err != nil {
		log.ErrorContext(ctx, "email_outbox: count pending failed", "error", err)
	} else {
		metrics.OutboxPendingRows.Set(float64(pending))
	}

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
			metrics.EmailsSentTotal.WithLabelValues(sendResult(sender)).Inc()
			markCtx, cancel := context.WithTimeout(context.Background(), outboxPersistTimeout)
			if mErr := repo.MarkSent(markCtx, row.ID, jobID); mErr != nil {
				metrics.OutboxStrandedSentRows.Inc()
				log.ErrorContext(ctx, "email_outbox: mark_sent failed after successful delivery — row may be re-sent on next tick (duplicate email risk)", "id", row.ID, "error", mErr)
			}
			cancel()
			continue
		}
		if errors.Is(sendErr, email.ErrPermanent) {
			metrics.EmailsDeadLetteredTotal.Inc()
			if mErr := repo.MarkFailed(ctx, row.ID, sendErr.Error()); mErr != nil {
				log.ErrorContext(ctx, "email_outbox: mark_failed failed", "id", row.ID, "error", mErr)
			}
			continue
		}
		if row.Attempts+1 >= maxAttempts {
			metrics.EmailsDeadLetteredTotal.Inc()
		} else {
			metrics.EmailsRescheduledTotal.Inc()
		}
		if rErr := repo.Reschedule(ctx, row.ID, row.Attempts, sendErr.Error(), maxAttempts); rErr != nil {
			log.ErrorContext(ctx, "email_outbox: reschedule failed", "id", row.ID, "error", rErr)
		}
	}
}

// sendResult maps a successful Sender.Send to its emails_sent_total result
// label. A NoopSender accepts the message without delivering it, so its sends
// are recorded as noop_dropped — dropped mail stays visible rather than being
// counted as a real delivery.
func sendResult(sender email.Sender) string {
	if _, ok := sender.(*email.NoopSender); ok {
		return "noop_dropped"
	}
	return "sent"
}
