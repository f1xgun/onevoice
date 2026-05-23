package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// auditLogsRetentionRunsTotal counts retention-sweep invocations by outcome.
//
// The "result" label is bounded to the closed set {ok, locked, error}:
//   - ok     — advisory lock acquired, DELETE ran (zero or more rows).
//   - locked — pg_try_advisory_lock returned false (another replica is
//     sweeping this tick). The sweep skipped to the next tick. Visible
//     in /metrics so multi-replica deployments can confirm the lock
//     contention behaves as expected.
//   - error  — lock acquisition failed OR DELETE failed (PG down,
//     network blip). The next 24h tick retries the sweep.
//
// Cardinality is fixed at 3 — Pitfall 7 of 19-RESEARCH.md.
var auditLogsRetentionRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "audit_logs_retention_runs_total",
	Help: "Audit-log retention sweep invocations, labeled by result {ok|locked|error}.",
}, []string{"result"})

// auditLogsRetentionDeletedTotal counts rows deleted by the retention
// sweep across all runs. Plain counter (no labels) — alerts watch the
// derivative ("sudden spike → likely backfill / data import; sustained
// zero for >7d → sweep broken").
var auditLogsRetentionDeletedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "audit_logs_retention_deleted_total",
	Help: "Total audit_logs rows deleted by the retention sweep across all runs.",
})

// IncRetentionRun increments the {result} bucket of
// audit_logs_retention_runs_total. result must be one of "ok", "locked",
// "error" — any other value still increments but pollutes the label set.
// Callers are expected to pass one of the three constants only.
func IncRetentionRun(result string) {
	auditLogsRetentionRunsTotal.WithLabelValues(result).Inc()
}

// AddRetentionDeleted increments audit_logs_retention_deleted_total by n.
// Negative or zero counts are no-ops; the counter is monotonic.
func AddRetentionDeleted(n int64) {
	if n <= 0 {
		return
	}
	auditLogsRetentionDeletedTotal.Add(float64(n))
}
