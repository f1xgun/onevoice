// Package wire — connection_health.go
//
// StartConnectionHealth runs the proactive connection-health worker: a
// background loop that re-probes each active Yandex session, records the
// fail-soft health verdict on the integration, and DMs the bound owner once on
// a fresh break. It mirrors StartOwnerBrief's lifecycle (enrolled on the
// shutdown WaitGroup, gated by an enabled flag + poll interval, observed via the
// sweeper_* metrics) and runs an immediate first pass on start so a deploy
// detects existing breaks promptly — the transition-only nudge gate + nudged_at
// throttle make the extra pass a cheap no-op for anything already handled.
package wire

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// StartConnectionHealth starts the connection-health loop. It is a no-op when
// the worker is nil OR enabled is false (CONNECTION_HEALTH_ENABLED=false), so a
// deploy can opt out. The goroutine is enrolled on wg (wg.Add before the spawn,
// wg.Done on return) so shutdown joins an in-flight pass before the database
// pools close, exactly like StartOwnerBrief. The loop stops when the parent ctx
// cancels or Services.Close is called.
func (s *Services) StartConnectionHealth(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, enabled bool, pollInterval time.Duration) {
	if s == nil || s.ConnectionHealth == nil {
		return
	}
	if !enabled {
		log.Info("connection health disabled (CONNECTION_HEALTH_ENABLED=false)")
		return
	}
	metrics.MarkSweeperSuccess(metrics.SweeperConnectionHealth)
	healthCtx, healthCancel := context.WithCancel(ctx)
	s.connectionHealthCancel = healthCancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		runConnectionHealthLoop(healthCtx, log, s.ConnectionHealth.RunOnce, pollInterval)
	}()
	log.Info("connection health started", "poll_interval", pollInterval.String())
}

// runConnectionHealthLoop drives the worker: one immediate pass, then one pass
// per tick. The fail-soft verdict + nudge throttle make the immediate pass safe
// on a fresh deploy or a mid-cycle restart. Per-pass errors are logged +
// metric'd but never abort the loop. Bound to ctx so SIGTERM / Close cancels the
// ticker cleanly.
func runConnectionHealthLoop(ctx context.Context, log *slog.Logger, fn func(context.Context) error, interval time.Duration) {
	connectionHealthPass(ctx, log, fn)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			connectionHealthPass(ctx, log, fn)
		}
	}
}

// connectionHealthPass runs one health pass and records its outcome on the
// sweeper_* metrics so a wedged or failing worker is alertable.
func connectionHealthPass(ctx context.Context, log *slog.Logger, fn func(context.Context) error) {
	if err := fn(ctx); err != nil {
		metrics.IncSweeperRun(metrics.SweeperConnectionHealth, metrics.SweeperResultError)
		log.WarnContext(ctx, "connection health pass failed", "err", err)
		return
	}
	metrics.IncSweeperRun(metrics.SweeperConnectionHealth, metrics.SweeperResultOK)
	metrics.MarkSweeperSuccess(metrics.SweeperConnectionHealth)
}
