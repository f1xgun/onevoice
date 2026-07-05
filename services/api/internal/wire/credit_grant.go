// Package wire — credit_grant.go
//
// StartCreditGrant runs the monthly credit-grant worker: a background loop that
// grants each active business its plan's monthly allowance, idempotently per
// (business, period). It mirrors StartReconciler's lifecycle (enrolled on the
// shutdown WaitGroup, gated by an enabled flag + poll interval, observed via the
// sweeper_* metrics) but runs an immediate first pass on start so balances
// populate promptly on deploy rather than only after the first tick.
package wire

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// StartCreditGrant starts the monthly credit-grant loop. It is a no-op when the
// grant service is nil OR enabled is false (CREDIT_GRANT_ENABLED=false), so a
// deploy can opt out. The goroutine is enrolled on wg (wg.Add before the spawn,
// wg.Done on return) so shutdown joins an in-flight grant pass before the
// database pools close, exactly like StartReconciler. The loop stops when the
// parent ctx cancels or Services.Close is called.
func (s *Services) StartCreditGrant(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, enabled bool, pollInterval time.Duration) {
	if s == nil || s.CreditGrant == nil {
		return
	}
	if !enabled {
		log.Info("credit grant disabled (CREDIT_GRANT_ENABLED=false)")
		return
	}
	metrics.MarkSweeperSuccess(metrics.SweeperCreditGrant)
	grantCtx, grantCancel := context.WithCancel(ctx)
	s.creditGrantCancel = grantCancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		runCreditGrantLoop(grantCtx, log, s.CreditGrant.GrantAll, pollInterval)
	}()
	log.Info("credit grant started", "poll_interval", pollInterval.String())
}

// runCreditGrantLoop drives the grant worker: one immediate pass, then one pass
// per tick. The immediate pass makes a fresh deploy (and a process restart at
// month rollover) land balances without waiting a full interval; the grant is
// idempotent, so the extra pass is cheap and self-healing. Per-pass errors are
// logged + metric'd but never abort the loop. Bound to ctx so SIGTERM / Close
// cancels the ticker cleanly. Same sweeper_* heartbeat idiom as
// runReconcileLoop / the cmd/main.go compliance sweepers.
func runCreditGrantLoop(ctx context.Context, log *slog.Logger, fn func(context.Context) (int, error), interval time.Duration) {
	creditGrantPass(ctx, log, fn)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			creditGrantPass(ctx, log, fn)
		}
	}
}

// creditGrantPass runs one grant pass and records its outcome on the sweeper_*
// metrics so a wedged or failing grant worker is alertable.
func creditGrantPass(ctx context.Context, log *slog.Logger, fn func(context.Context) (int, error)) {
	n, err := fn(ctx)
	if err != nil {
		metrics.IncSweeperRun(metrics.SweeperCreditGrant, metrics.SweeperResultError)
		log.WarnContext(ctx, "credit grant pass failed", "err", err)
		return
	}
	metrics.IncSweeperRun(metrics.SweeperCreditGrant, metrics.SweeperResultOK)
	metrics.MarkSweeperSuccess(metrics.SweeperCreditGrant)
	if n > 0 {
		metrics.AddSweeperItems(metrics.SweeperCreditGrant, n)
		log.InfoContext(ctx, "credit grant: businesses granted", "count", n)
	}
}
