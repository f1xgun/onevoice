package audit

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// auditLogWriteFailuresTotal counts terminal write failures (all 3 retries
// exhausted). Label action_category is bounded to the closed set returned
// by ActionCategory() — see Pitfall 7 in 19-RESEARCH.md.
var auditLogWriteFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "audit_log_write_failures_total",
	Help: "Total audit_logs INSERT failures after retry exhaustion, by action category.",
}, []string{"action_category"})

// IncWriteFailure increments the counter for the action's category.
// Exported so tests can verify without touching the package-private var.
func IncWriteFailure(action string) {
	auditLogWriteFailuresTotal.WithLabelValues(ActionCategory(action)).Inc()
}
