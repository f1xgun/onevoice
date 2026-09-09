package wire

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const weeklyValueRecapLockSQL = `SELECT pg_try_advisory_xact_lock(hashtext('weekly_value_recap')::bigint)`
const weeklyValueRecapPassTimeout = 5 * time.Minute

func (s *Services) StartWeeklyValueRecap(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, pool *pgxpool.Pool, enabled bool, interval time.Duration) {
	if s == nil || s.WeeklyValueRecap == nil || pool == nil || !enabled {
		return
	}
	metrics.MarkSweeperSuccess(metrics.SweeperWeeklyValueRecap)
	workerCtx, cancel := context.WithCancel(ctx)
	s.weeklyValueRecapCancel = cancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		runWeeklyValueRecap(workerCtx, log, pool, s.WeeklyValueRecap.Sweep, interval)
	}()
}

func runWeeklyValueRecap(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool, sweep func(context.Context, time.Time) (int, error), interval time.Duration) {
	runWeeklyValueRecapPass(ctx, log, pool, sweep)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runWeeklyValueRecapPass(ctx, log, pool, sweep)
		}
	}
}

func runWeeklyValueRecapPass(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool, sweep func(context.Context, time.Time) (int, error)) {
	passCtx, cancel := context.WithTimeout(ctx, weeklyValueRecapPassTimeout)
	defer cancel()
	tx, err := pool.BeginTx(passCtx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		metrics.IncSweeperRun(metrics.SweeperWeeklyValueRecap, metrics.SweeperResultError)
		log.WarnContext(ctx, "weekly recap transaction failed", "error", err)
		return
	}
	defer func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.WithoutCancel(passCtx), 5*time.Second)
		defer rollbackCancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	var acquired bool
	if err := tx.QueryRow(passCtx, weeklyValueRecapLockSQL).Scan(&acquired); err != nil || !acquired {
		if err != nil {
			metrics.IncSweeperRun(metrics.SweeperWeeklyValueRecap, metrics.SweeperResultError)
			log.WarnContext(ctx, "weekly recap lock failed", "error", err)
		}
		return
	}
	count, err := sweep(passCtx, time.Now().UTC())
	if err != nil {
		metrics.AddSweeperItems(metrics.SweeperWeeklyValueRecap, count)
		metrics.IncSweeperRun(metrics.SweeperWeeklyValueRecap, metrics.SweeperResultError)
		log.WarnContext(ctx, "weekly recap sweep failed", "error", err)
		return
	}
	if err := tx.Commit(passCtx); err != nil {
		metrics.IncSweeperRun(metrics.SweeperWeeklyValueRecap, metrics.SweeperResultError)
		log.WarnContext(ctx, "weekly recap lock commit failed", "error", err)
		return
	}
	metrics.IncSweeperRun(metrics.SweeperWeeklyValueRecap, metrics.SweeperResultOK)
	metrics.MarkSweeperSuccess(metrics.SweeperWeeklyValueRecap)
	metrics.AddSweeperItems(metrics.SweeperWeeklyValueRecap, count)
	if count > 0 {
		log.InfoContext(ctx, "weekly recap rows written", "count", count)
	}
}
