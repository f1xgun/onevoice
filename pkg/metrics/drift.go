package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Proactive platform-sync (reconciliation) observability. The reconciler
// periodically fetches each connected channel's live profile and compares it
// against what OneVoice pushed; these collectors make drift and reconcile
// health alertable. See pkg/metrics/README.md for the label-cardinality rules.

// Reconcile result values — the bounded value set for the {result} label.
const (
	// ReconcileResultOK marks a fetch+compare that completed with no drift.
	ReconcileResultOK = "ok"
	// ReconcileResultDrift marks a fetch+compare that found drifted fields.
	ReconcileResultDrift = "drift"
	// ReconcileResultError marks a fetch that failed (transport / API / RPA).
	ReconcileResultError = "error"
)

// syncDriftDetected is 1 when a platform's copy has drifted from the stored
// business profile, 0 otherwise, per {platform}. The reconciler sets it on
// every check so a repaired channel flips back to 0. {platform} is the fixed
// three-value set {telegram, vk, yandex_business}; never derive it from a
// runtime value.
var syncDriftDetected = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sync_drift_detected",
	Help: "1 when the platform copy has drifted from the stored business profile, per {platform}.",
}, []string{"platform"})

// syncReconcileChecksTotal counts reconcile passes by {platform} and
// {result=ok|error|drift}. Cardinality = 3 platforms x 3 results.
var syncReconcileChecksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sync_reconcile_checks_total",
	Help: "Proactive sync reconcile checks, labeled by {platform} and {result=ok|error|drift}.",
}, []string{"platform", "result"})

// syncReconcileFetchDuration observes the wall-clock cost of one remote fetch
// per {platform} — the Telegram/VK direct API call or the Yandex RPA dispatch.
var syncReconcileFetchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "sync_reconcile_fetch_duration_seconds",
	Help:    "Remote profile fetch duration in seconds, per {platform}.",
	Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 90},
}, []string{"platform"})

// SetSyncDrift records the current drift state for a platform. platform must be
// one of the bounded platform constants; callers pass literals only.
func SetSyncDrift(platform string, drifted bool) {
	v := 0.0
	if drifted {
		v = 1.0
	}
	syncDriftDetected.WithLabelValues(platform).Set(v)
}

// IncReconcileCheck records one reconcile check outcome. platform and result
// must be one of the bounded constants above.
func IncReconcileCheck(platform, result string) {
	syncReconcileChecksTotal.WithLabelValues(platform, result).Inc()
}

// ObserveReconcileFetch records the duration of one remote fetch for platform.
func ObserveReconcileFetch(platform string, d time.Duration) {
	syncReconcileFetchDuration.WithLabelValues(platform).Observe(d.Seconds())
}

// GetReconcileCheckCounter exposes the sync_reconcile_checks_total collector for
// test assertions (testutil.ToFloat64). Not for production call sites.
func GetReconcileCheckCounter() *prometheus.CounterVec {
	return syncReconcileChecksTotal
}
