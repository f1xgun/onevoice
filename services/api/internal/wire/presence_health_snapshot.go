// Package wire — presence_health_snapshot.go
//
// StartPresenceHealthSnapshot runs the weekly presence-health snapshot worker:
// a background loop that stamps each active business's composite score for the
// current ISO-week, idempotently per (business, week). It mirrors
// StartCreditGrant's lifecycle (enrolled on the shutdown WaitGroup, gated by an
// enabled flag + poll interval, observed via the sweeper_* metrics) and runs an
// immediate first pass on start so a deploy lands this week's snapshots promptly
// — the per-week UNIQUE + upsert makes the extra pass a cheap no-op for anything
// already stamped this week.
package wire

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// StartPresenceHealthSnapshot starts the weekly snapshot loop. It is a no-op
// when the service is nil OR enabled is false
// (PRESENCE_HEALTH_SNAPSHOT_ENABLED=false), so a deploy can opt out. The
// goroutine is enrolled on wg (wg.Add before the spawn, wg.Done on return) so
// shutdown joins an in-flight pass before the database pools close, exactly like
// StartCreditGrant. The loop stops when the parent ctx cancels or Services.Close
// is called.
func (s *Services) StartPresenceHealthSnapshot(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, enabled bool, pollInterval time.Duration) {
	if s == nil || s.PresenceHealthSnapshot == nil {
		return
	}
	if !enabled {
		log.Info("presence health snapshot disabled (PRESENCE_HEALTH_SNAPSHOT_ENABLED=false)")
		return
	}
	metrics.MarkSweeperSuccess(metrics.SweeperPresenceHealth)
	snapCtx, snapCancel := context.WithCancel(ctx)
	s.presenceHealthSnapshotCancel = snapCancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		runPresenceHealthSnapshotLoop(snapCtx, log, s.PresenceHealthSnapshot.SnapshotAll, pollInterval)
	}()
	log.Info("presence health snapshot started", "poll_interval", pollInterval.String())
}

// runPresenceHealthSnapshotLoop drives the worker: one immediate pass, then one
// pass per tick. The per-week idempotency stamp makes the immediate pass safe on
// a fresh deploy or a mid-week restart. Per-pass errors are logged + metric'd but
// never abort the loop. Bound to ctx so SIGTERM / Close cancels the ticker
// cleanly.
func runPresenceHealthSnapshotLoop(ctx context.Context, log *slog.Logger, fn func(context.Context) (int, error), interval time.Duration) {
	presenceHealthSnapshotPass(ctx, log, fn)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			presenceHealthSnapshotPass(ctx, log, fn)
		}
	}
}

// presenceHealthSnapshotPass runs one snapshot pass and records its outcome on
// the sweeper_* metrics so a wedged or failing worker is alertable.
func presenceHealthSnapshotPass(ctx context.Context, log *slog.Logger, fn func(context.Context) (int, error)) {
	n, err := fn(ctx)
	if err != nil {
		metrics.IncSweeperRun(metrics.SweeperPresenceHealth, metrics.SweeperResultError)
		log.WarnContext(ctx, "presence health snapshot pass failed", "err", err)
		return
	}
	metrics.IncSweeperRun(metrics.SweeperPresenceHealth, metrics.SweeperResultOK)
	metrics.MarkSweeperSuccess(metrics.SweeperPresenceHealth)
	if n > 0 {
		metrics.AddSweeperItems(metrics.SweeperPresenceHealth, n)
		log.InfoContext(ctx, "presence health snapshot: businesses stamped", "count", n)
	}
}
