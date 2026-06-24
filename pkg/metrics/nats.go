package metrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// NATS collectors emit publish + handler RED signals for the a2a transport.
//
// Cardinality budget — labels MUST stay in the documented allowlist (see
// pkg/metrics/README.md). Allowed values for `subject`:
//   - tasks.telegram, tasks.vk, tasks.yandex_business, tasks.google_business
//   - "_INBOX" (collapsed from _INBOX.<nuid> auto-reply subjects)
//
// Allowed values for `result`: ok, error, timeout.
// Banlist: business_id, user_id, email, conversation_id, request_id, raw
// _INBOX.<nuid>. Always pass subject through CollapseSubject before recording.
var (
	natsPublishTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nats_publish_total",
		Help: "Total NATS publishes by subject and result.",
	}, []string{"subject", "result"})

	natsPublishDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nats_publish_duration_seconds",
		Help:    "NATS publish call duration in seconds.",
		Buckets: []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5},
	}, []string{"subject"})

	natsHandlerDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nats_handler_duration_seconds",
		Help:    "Server-side NATS handler execution duration in seconds.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"subject", "result"})
)

// CollapseSubject normalizes NATS reply subjects so cardinality stays bounded.
// _INBOX.<nuid> auto-reply subjects collapse to the literal "_INBOX". All
// other subjects pass through unchanged.
func CollapseSubject(subject string) string {
	if strings.HasPrefix(subject, "_INBOX.") {
		return "_INBOX"
	}
	return subject
}

// A2AHandlersInflight is a per-process gauge of in-flight a2a message handlers
// (== concurrency slots held when a cap is configured). It pins at the
// configured cap when an agent is saturated (backpressure active) — the signal
// that A2A_MAX_CONCURRENT is the binding constraint. No labels: one capacity
// signal per agent process.
var A2AHandlersInflight = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "a2a_handlers_inflight",
	Help: "Current number of in-flight a2a message handlers in this agent process (concurrency slots held when a cap is set).",
})

// a2aHandlerPanicsTotal counts panics recovered at the a2a message-handler
// boundary, labeled by {agent_id}. A non-zero rate means a handler/SDK/RPA path
// panicked on an incoming request — always a bug worth paging on. Cardinality
// is fixed at the closed agent_id set (telegram, vk, yandex_business,
// google_business) via IncA2AHandlerPanic; see pkg/metrics/README.md.
var a2aHandlerPanicsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "a2a_handler_panics_total",
	Help: "Panics recovered at the a2a message-handler boundary, labeled by {agent_id}.",
}, []string{"agent_id"})

// IncA2AHandlerPanic records one recovered handler panic for agentID. agentID
// must be one of the closed AgentID set; any other value is normalized to
// "unknown" so a stray caller can never explode label cardinality.
func IncA2AHandlerPanic(agentID string) {
	switch agentID {
	case "telegram", "vk", "yandex_business", "google_business":
	default:
		agentID = "unknown"
	}
	a2aHandlerPanicsTotal.WithLabelValues(agentID).Inc()
}

// RecordNATSPublish records a single NATS publish attempt.
func RecordNATSPublish(subject, result string, duration time.Duration) {
	s := CollapseSubject(subject)
	natsPublishTotal.WithLabelValues(s, result).Inc()
	natsPublishDuration.WithLabelValues(s).Observe(duration.Seconds())
}

// RecordNATSHandler records server-side handler execution time.
func RecordNATSHandler(subject, result string, duration time.Duration) {
	natsHandlerDuration.WithLabelValues(CollapseSubject(subject), result).Observe(duration.Seconds())
}
