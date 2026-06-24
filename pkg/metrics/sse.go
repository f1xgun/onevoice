package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SSEConcurrencyBlocked counts per-user SSE concurrency rejections,
// labeled by tier so operators can see which tier is hitting the cap.
// No user_id label by design — cardinality + PII.
var SSEConcurrencyBlocked = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sse_concurrency_blocked_total",
	Help: "Number of SSE chat requests rejected because the user is at the per-user concurrency cap.",
}, []string{"tier"})

// SSEConcurrencyInflight is a process-wide gauge of currently held SSE
// concurrency slots. Increments on Acquire, decrements on release. No
// labels — it is a single global capacity signal.
var SSEConcurrencyInflight = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "sse_concurrency_inflight",
	Help: "Current number of in-flight SSE chat streams holding a per-user concurrency slot in this process.",
})

// SSEConcurrencyRollbackFailed counts over-cap rollback DECRs that failed
// during Acquire's over-cap rejection path. Labeled by tier so operators
// can correlate Redis-brownout signal with the affected user tier. A
// non-zero rate during a Redis incident indicates leaked slots that the
// TTL will eventually reclaim — useful for postmortem timing.
var SSEConcurrencyRollbackFailed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sse_concurrency_rollback_failed_total",
	Help: "Number of over-cap rollback DECR operations that failed; the counter slot is reclaimed by TTL.",
}, []string{"tier"})

// OrchestratorStreamsInflight is the orchestrator's process-wide gauge of SSE
// streams currently holding a GLOBAL concurrency slot. Distinct from the
// per-user, Redis-backed SSEConcurrency* metrics above (which live on the api):
// this is a single backstop limit on total concurrent streams a single
// orchestrator process will serve. No labels — one global capacity signal.
var OrchestratorStreamsInflight = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "orchestrator_streams_inflight",
	Help: "Current number of in-flight orchestrator SSE streams holding a global concurrency slot.",
})

// OrchestratorStreamsRejectedTotal counts orchestrator SSE requests rejected
// with 503 because the process-wide stream concurrency cap was full.
var OrchestratorStreamsRejectedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "orchestrator_streams_rejected_total",
	Help: "Number of orchestrator SSE requests rejected because the global stream concurrency cap was reached.",
})
