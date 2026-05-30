// Package wire — retention_sweep.go
//
// Audit-log retention enforcement: a Go goroutine that ticks every 24h and
// runs `DELETE FROM audit_logs WHERE created_at < now - 365d`. Replaces
// the original PLAN's pg_cron approach (REVISED 2026-05-22 —
// the project's postgres:16-alpine image does not ship pg_cron and Alpine
// is not supported upstream). See 19-RESEARCH.md "Open Item 1" for the
// reasoning trail.
//
// Multi-replica safety: every tick acquires
// `pg_try_advisory_lock(hashtext('audit_logs_retention')::bigint)`. Only
// the replica that wins the lock runs the DELETE; the loser increments
// {result="locked"} and skips to the next tick. The advisory-lock key
// namespace is application-private (hashtext of a deliberately unique
// string), so it cannot collide with another sweep elsewhere in the codebase
//
// Lifecycle: StartRetentionSweep spawns the goroutine and returns
// immediately. The goroutine exits when its ctx is canceled (the api's
// signal-derived rootCtx — i.e., SIGTERM / SIGINT during graceful
// shutdown). Errors during sweep never propagate up — they are slog'd +
// metric'd so the API stays healthy if PG is briefly unreachable.
package wire

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

const (
	// retentionPeriod is the maximum age (365 days).
	retentionPeriod = 365 * 24 * time.Hour
	// retentionTick is the sweep cadence. 24h matches the original
	// (defunct) pg_cron schedule from the PLAN.
	retentionTick = 24 * time.Hour
	// retentionWarmup delays the first sweep so liveness/readiness probes
	// pass before any DB-heavy startup work. Matches the policy_sweep
	// retry interval — 60s is plenty for k8s readiness gates.
	retentionWarmup = 60 * time.Second

	// advisoryLockSQL acquires the per-application advisory lock that
	// gates the DELETE across replicas. hashtext collapses the string
	// key into a stable bigint Postgres uses internally. ::bigint avoids
	// the int4 overflow path that two-arg pg_try_advisory_lock takes.
	advisoryLockSQL = "SELECT pg_try_advisory_lock(hashtext('audit_logs_retention')::bigint)"
	// advisoryUnlockSQL releases the same lock. Always called in defer
	// after a successful acquire; we don't check the bool return because
	// pg_advisory_unlock returns false only when the lock isn't held by
	// the current session — i.e., already a no-op for us.
	advisoryUnlockSQL = "SELECT pg_advisory_unlock(hashtext('audit_logs_retention')::bigint)"
)

// lockExecutor is the narrow pool subset that sweep needs: QueryRow for
// the pg_try_advisory_lock bool and Exec for the unlock. *pgxpool.Pool
// satisfies it directly; pgxmock.PgxPoolIface satisfies it in tests. This
// is the same dependency-injection pattern as repository.pgxPool (pool.go).
type lockExecutor interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// StartRetentionSweep spawns the retention goroutine and returns
// immediately so it never blocks API startup. The goroutine exits when
// ctx is canceled (the API's signal-derived rootCtx fires on SIGTERM /
// SIGINT). Errors are observed via slog + audit_logs_retention_runs_total
// counter — they never propagate up.
//
// Idempotent at the lock level: calling StartRetentionSweep twice on the
// same process spawns two goroutines, but only one will hold the
// advisory lock at any tick. In a single-process deployment, do not
// invoke twice — there is no reason to.
func StartRetentionSweep(ctx context.Context, pool lockExecutor, repo domain.AuditLogRepository) {
	go runRetention(ctx, pool, repo)
}

// runRetention is the goroutine body. Split out so StartRetentionSweep
// stays a thin entry point and runRetention can be unit-tested if needed
// (the load-bearing logic is in sweep, which has its own tests).
func runRetention(ctx context.Context, pool lockExecutor, repo domain.AuditLogRepository) {
	// Warmup window before the first sweep — readiness probes pass first,
	// and the API has time to recover from any startup PG hiccups before
	// we issue a long-running DELETE.
	select {
	case <-ctx.Done():
		return
	case <-time.After(retentionWarmup):
	}

	// First sweep immediately after warmup; subsequent sweeps on the
	// 24h ticker. Aligns with the original pg_cron "0 3 * * *" cadence —
	// the absolute clock time of the first tick doesn't matter for a
	// 365d cutoff.
	sweep(ctx, pool, repo)
	t := time.NewTicker(retentionTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep(ctx, pool, repo)
		}
	}
}

// sweep runs exactly one retention pass: acquire advisory lock, DELETE
// expired rows, release lock. Every failure path is observable via
// metrics + slog; sweep itself never panics or returns errors.
//
// Exported package-private (lowercase) for the test file in this package
// — production callers should use StartRetentionSweep.
func sweep(ctx context.Context, pool lockExecutor, repo domain.AuditLogRepository) {
	var acquired bool
	if err := pool.QueryRow(ctx, advisoryLockSQL).Scan(&acquired); err != nil {
		metrics.IncRetentionRun("error")
		slog.ErrorContext(ctx, "audit_logs retention: lock acquire failed", "error", err)
		return
	}
	if !acquired {
		// Another replica is sweeping this tick; expected behavior in a
		// multi-replica deployment. Debug-level log (not warn) — this
		// is steady-state, not an incident.
		metrics.IncRetentionRun("locked")
		slog.DebugContext(ctx, "audit_logs retention: lock held by another replica, skipping")
		return
	}
	defer func() {
		if _, err := pool.Exec(ctx, advisoryUnlockSQL); err != nil {
			// Unlock failure is non-fatal — the session-scoped lock is
			// released automatically when the pgx connection returns to
			// the pool. Warn-level so it shows up on dashboards but
			// doesn't page.
			slog.WarnContext(ctx, "audit_logs retention: unlock failed", "error", err)
		}
	}()

	cutoff := time.Now().Add(-retentionPeriod)
	n, err := repo.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		metrics.IncRetentionRun("error")
		slog.ErrorContext(ctx, "audit_logs retention: delete failed", "error", err, "cutoff", cutoff)
		return
	}
	metrics.IncRetentionRun("ok")
	metrics.AddRetentionDeleted(n)
	slog.InfoContext(ctx, "audit_logs retention swept", "deleted", n, "cutoff", cutoff)
}
