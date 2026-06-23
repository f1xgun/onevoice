package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Background-sweeper observability. The deletion/warning sweepers in
// services/api/cmd/main.go are compliance-critical (152-FZ 30-day purge,
// T-7 deletion-warning mail) and previously emitted only slog lines, so a
// wedged sweeper failed invisibly. These collectors make a stuck sweeper
// alertable. See pkg/metrics/README.md for the label-cardinality rules.

// Sweeper names — the bounded value set for the {sweeper} label. Each is a
// distinct background job hard-coded at the call site; never derive from a
// runtime variable.
const (
	SweeperAccountHardDelete  = "account_hard_delete"
	SweeperBusinessHardDelete = "business_hard_delete"
	SweeperDeletionWarning    = "deletion_warning"
)

// Sweeper run outcomes — the bounded value set for the {result} label.
const (
	SweeperResultOK    = "ok"
	SweeperResultError = "error"
)

// sweeperRunsTotal counts sweeper passes by {sweeper} and {result}. result is
// the closed set {ok, error}: ok = the pass completed without error (zero or
// more items acted upon); error = the service call returned an error and the
// pass is retried on the next tick. Cardinality = 3 sweepers x 2 results.
var sweeperRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sweeper_runs_total",
	Help: "Background sweeper passes, labeled by {sweeper} and {result=ok|error}.",
}, []string{"sweeper", "result"})

// sweeperItemsProcessedTotal counts the items a sweeper acted upon across all
// passes (users/orgs hard-deleted, T-7 warning mails enqueued), labeled by
// {sweeper}. Alerts watch the derivative, not the absolute value.
var sweeperItemsProcessedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sweeper_items_processed_total",
	Help: "Items acted upon by background sweepers, labeled by {sweeper}.",
}, []string{"sweeper"})

// sweeperLastSuccessTimestamp is the Unix time of the last error-free pass per
// {sweeper}. It is seeded at process start (see MarkSweeperSuccess) so that a
// sweeper which never completes a single pass still ages past the staleness
// threshold and fires — the same heartbeat idiom as backup_last_success_timestamp.
var sweeperLastSuccessTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sweeper_last_success_timestamp",
	Help: "Unix timestamp of the last error-free pass per {sweeper} (seeded at startup).",
}, []string{"sweeper"})

// IncSweeperRun records one sweeper pass outcome. sweeper and result must be
// one of the bounded constants above; callers pass literals only, so the label
// set stays fixed (see README.md "Adding a new collector").
func IncSweeperRun(sweeper, result string) {
	sweeperRunsTotal.WithLabelValues(sweeper, result).Inc()
}

// AddSweeperItems adds n to the items-processed counter for sweeper. Negative
// or zero counts are no-ops; the counter is monotonic.
func AddSweeperItems(sweeper string, n int) {
	if n <= 0 {
		return
	}
	sweeperItemsProcessedTotal.WithLabelValues(sweeper).Add(float64(n))
}

// MarkSweeperSuccess stamps the last-error-free-pass gauge for sweeper to now.
// Call it once at startup to seed the heartbeat (so the absent/staleness alert
// is deploy-safe), then after every error-free pass.
func MarkSweeperSuccess(sweeper string) {
	sweeperLastSuccessTimestamp.WithLabelValues(sweeper).Set(float64(time.Now().Unix()))
}
