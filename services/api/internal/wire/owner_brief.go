// Package wire — owner_brief.go
//
// StartOwnerBrief runs the weekly owner-brief worker: a background loop that
// composes and dispatches a proactive weekly summary DM to each eligible
// business owner's private Telegram chat. It mirrors StartCreditGrant's
// lifecycle (enrolled on the shutdown WaitGroup, gated by an enabled flag + poll
// interval, observed via the sweeper_* metrics) but runs an immediate first pass
// on start so a deploy lands due briefs promptly rather than only after the
// first tick — the per-week idempotency stamp makes the extra pass a cheap no-op
// for anything already sent this week.
package wire

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// StartOwnerBrief starts the weekly owner-brief loop. It is a no-op when the
// service is nil OR enabled is false (OWNER_BRIEF_ENABLED=false), so a deploy can
// opt out. The goroutine is enrolled on wg (wg.Add before the spawn, wg.Done on
// return) so shutdown joins an in-flight pass before the database pools close,
// exactly like StartCreditGrant. The loop stops when the parent ctx cancels or
// Services.Close is called.
func (s *Services) StartOwnerBrief(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, enabled bool, pollInterval time.Duration) {
	if s == nil || s.OwnerBrief == nil {
		return
	}
	if !enabled {
		log.Info("owner brief disabled (OWNER_BRIEF_ENABLED=false)")
		return
	}
	metrics.MarkSweeperSuccess(metrics.SweeperOwnerBrief)
	briefCtx, briefCancel := context.WithCancel(ctx)
	s.ownerBriefCancel = briefCancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		runOwnerBriefLoop(briefCtx, log, s.OwnerBrief.RunOnce, pollInterval)
	}()
	log.Info("owner brief started", "poll_interval", pollInterval.String())
}

// runOwnerBriefLoop drives the worker: one immediate pass, then one pass per
// tick. The per-week idempotency stamp makes the immediate pass safe on a fresh
// deploy or a mid-week restart. Per-pass errors are logged + metric'd but never
// abort the loop. Bound to ctx so SIGTERM / Close cancels the ticker cleanly.
func runOwnerBriefLoop(ctx context.Context, log *slog.Logger, fn func(context.Context) error, interval time.Duration) {
	ownerBriefPass(ctx, log, fn)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ownerBriefPass(ctx, log, fn)
		}
	}
}

// ownerBriefPass runs one brief pass and records its outcome on the sweeper_*
// metrics so a wedged or failing worker is alertable.
func ownerBriefPass(ctx context.Context, log *slog.Logger, fn func(context.Context) error) {
	if err := fn(ctx); err != nil {
		metrics.IncSweeperRun(metrics.SweeperOwnerBrief, metrics.SweeperResultError)
		log.WarnContext(ctx, "owner brief pass failed", "err", err)
		return
	}
	metrics.IncSweeperRun(metrics.SweeperOwnerBrief, metrics.SweeperResultOK)
	metrics.MarkSweeperSuccess(metrics.SweeperOwnerBrief)
}
