package wire

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

const (
	integrationsRetentionPeriod      = 90 * 24 * time.Hour
	integrationsPurgeTick            = 24 * time.Hour
	integrationsPurgeWarmup          = 60 * time.Second
	integrationsPurgeAdvisoryLockSQL = "SELECT pg_try_advisory_lock(hashtext('integrations_purge')::bigint)"
	integrationsPurgeAdvisoryUnlock  = "SELECT pg_advisory_unlock(hashtext('integrations_purge')::bigint)"
)

// integrationsPurgeExecutor is the narrow pool subset the purge sweep needs:
// QueryRow for the advisory-lock bool and Exec for the unlock. Both
// *pgxpool.Pool and pgxmock.PgxPoolIface satisfy it.
type integrationsPurgeExecutor interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// integrationsPurger is the repository capability the sweep exercises:
// hard-deleting soft-deleted integrations older than the retention cutoff.
type integrationsPurger interface {
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// StartIntegrationsPurge spawns the purge goroutine and returns immediately so
// it never blocks API startup. The goroutine exits when ctx is canceled.
//
// The goroutine is registered on wg so the shutdown sequence can join it
// before the database pool closes (a sweep mid-pass must not Exec on a closed
// *pgxpool.Pool).
func StartIntegrationsPurge(ctx context.Context, wg *sync.WaitGroup, pool integrationsPurgeExecutor, repo integrationsPurger) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		runIntegrationsPurge(ctx, pool, repo)
	}()
}

func runIntegrationsPurge(ctx context.Context, pool integrationsPurgeExecutor, repo integrationsPurger) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(integrationsPurgeWarmup):
	}

	sweepIntegrations(ctx, pool, repo)
	t := time.NewTicker(integrationsPurgeTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweepIntegrations(ctx, pool, repo)
		}
	}
}

func sweepIntegrations(ctx context.Context, pool integrationsPurgeExecutor, repo integrationsPurger) {
	var locked bool
	if err := pool.QueryRow(ctx, integrationsPurgeAdvisoryLockSQL).Scan(&locked); err != nil {
		metrics.IncIntegrationsPurgeRun("error")
		slog.ErrorContext(ctx, "integrations purge: acquire advisory lock failed", "error", err)
		return
	}
	if !locked {
		metrics.IncIntegrationsPurgeRun("locked")
		return
	}
	defer func() {
		if _, err := pool.Exec(ctx, integrationsPurgeAdvisoryUnlock); err != nil {
			slog.WarnContext(ctx, "integrations purge: release advisory lock failed", "error", err)
		}
	}()

	cutoff := time.Now().Add(-integrationsRetentionPeriod)
	n, err := repo.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		metrics.IncIntegrationsPurgeRun("error")
		slog.ErrorContext(ctx, "integrations purge: delete failed", "cutoff", cutoff, "error", err)
		return
	}
	metrics.IncIntegrationsPurgeRun("ok")
	metrics.AddIntegrationsPurged(n)
}
