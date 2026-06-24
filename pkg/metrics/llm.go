package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	llmRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_requests_total",
		Help: "Total number of LLM requests.",
	}, []string{"model", "provider", "status"})

	llmRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_request_duration_seconds",
		Help:    "LLM request duration in seconds.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"model", "provider"})

	// llmCacheReadTokens / llmCacheCreateTokens / llmInputTokensAfterBreakpoint
	// expose Anthropic prompt-cache token breakdown for Grafana to compute
	// `llm_cache_hit_ratio`. Labeled by `model` only — MUST NOT add user,
	// business, or content labels (cardinality / PII mitigation: label
	// expansion requires PII review).
	llmCacheReadTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_cache_read_tokens_total",
		Help: "Total input tokens served from Anthropic prompt cache.",
	}, []string{"model"})

	llmCacheCreateTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_cache_create_tokens_total",
		Help: "Total input tokens written to Anthropic prompt cache (creation cost paid).",
	}, []string{"model"})

	llmInputTokensAfterBreakpoint = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_input_tokens_after_breakpoint_total",
		Help: "Total input tokens consumed after the last cache breakpoint (paid full input price).",
	}, []string{"model"})

	// BillingPostFailures counts every non-2xx outcome from
	// pkg/billingclient.Client.LogUsage. Labeled by reason so Grafana can
	// distinguish the silent-drop modes:
	//
	//	transient         — network failure / HTTP 5xx; safe to retry
	//	invalid_payload   — local validation failure / HTTP 400; do NOT retry
	//	unexpected_status — non-2xx, non-4xx, non-5xx (e.g. stray 418); likely
	//	                    a misconfigured reverse proxy
	//
	// The counter is the audit signal for the silent-loss billing posture
	// (see pkg/billingclient/AGENTS.md): the router drops the error on the
	// floor so the LLM response is never blocked, but the counter rises
	// every time a usage row is lost. v1.4 free-beta accepts this trade-off;
	// v1.5+ may add an outbox once the drop rate becomes material.
	//
	// SECURITY: labels MUST stay coarse (3 fixed values); do NOT add
	// business_id / user_id / model labels — cardinality blowout + PII.
	BillingPostFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_billing_post_failures_total",
		Help: "Count of billingclient.LogUsage failures, by reason (transient|invalid_payload|unexpected_status).",
	}, []string{"reason"})

	// LLMDailySpendBlocked counts chat requests blocked because the per-business
	// daily-spend cap was reached. Labeled by `tier` so Grafana can split the
	// alert per pricing plan.
	LLMDailySpendBlocked = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_daily_spend_blocked_total",
		Help: "Chat requests blocked because the per-business daily spend cap was reached, by tier.",
	}, []string{"tier"})

	// LLMConversationCapHit counts agent-loop terminations because the per-
	// conversation token cap was reached. `axis` is "input" or "output" so an
	// operator can distinguish prompt-heavy from completion-heavy runaways.
	LLMConversationCapHit = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_conversation_cap_hit_total",
		Help: "Agent-loop terminations because the per-conversation token cap was reached, by axis (input or output).",
	}, []string{"axis"})

	// LLMRedisDownFallback records the outcome whenever the rate-limiter sees
	// a Redis failure. `action` separates the four policy paths:
	//   block             — fail-closed default; the chat is refused.
	//   fallback          — in-process bucket allowed the request.
	//   fallback_blocked  — in-process bucket was exhausted.
	//   misconfigured     — local_fallback policy but no bucket was wired,
	//                       or the daily-spend lookup itself failed (we cannot
	//                       distinguish "rate-limiter cannot decide safely"
	//                       sources at the label level).
	LLMRedisDownFallback = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_redis_down_fallback_total",
		Help: "Rate-limiter Redis-failure outcomes, by action (block, fallback, fallback_blocked, misconfigured).",
	}, []string{"action"})

	// LLMRouterRetry records the outcome of the router's retry-once policy
	// when a transient provider error fires. `result` is one of
	// success / retrying / exhausted / nonretryable; `attempt` is first /
	// second so a Grafana panel can show "what fraction of first-attempt
	// transients recover on the sibling provider?".
	LLMRouterRetry = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_router_retry_total",
		Help: "Router retry-once outcomes, by result (success, retrying, exhausted, nonretryable) and attempt (first, second).",
	}, []string{"result", "attempt"})

	// LLMExpireFailure counts every time the rate-limiter finds an existing
	// per-user counter that has no TTL and re-stamps it (self-heal). The counter
	// increment + conditional expiry are now a single atomic Lua round trip, so
	// a TTL-less key is repaired on the very next request rather than blocking
	// the user until manual Redis intervention. This metric stays as the signal
	// that such a repair happened (a prior expiry was lost) so an operator can
	// still alert on the underlying Redis instability; the request itself is
	// always allowed through.
	//
	// `gate` is the closed set of rate-limit windows the limiter maintains:
	//   requests_min — per-minute request counter
	//   tokens_min   — per-minute token counter
	//   tokens_month — per-month token counter
	//
	// SECURITY: `gate` MUST stay in that 3-value set; do NOT add user_id /
	// business_id labels (cardinality blowout + PII). See pkg/metrics/README.md.
	LLMExpireFailure = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_expire_failure_total",
		Help: "Rate-limiter self-heals of a TTL-less counter, by gate (requests_min, tokens_min, tokens_month). A missing TTL is re-stamped on the next request.",
	}, []string{"gate"})
)

// RecordLLMRequest records a completed LLM request.
func RecordLLMRequest(model, provider, status string, duration time.Duration) {
	llmRequestsTotal.WithLabelValues(model, provider, status).Inc()
	llmRequestDuration.WithLabelValues(model, provider).Observe(duration.Seconds())
}

// llmFirstTokenLatency measures the time from ChatStream request start to the
// first streamed token reaching the caller. Labeled by {model, provider} only
// — see pkg/metrics/README.md for the cardinality allowlist; never add
// per-user / per-business / per-request labels here.
var llmFirstTokenLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "llm_first_token_latency_seconds",
	Help:    "Time from ChatStream request start to first streamed token in seconds.",
	Buckets: []float64{0.1, 0.25, 0.5, 0.75, 1, 1.5, 2.5, 5, 10},
}, []string{"model", "provider"})

// RecordLLMFirstToken records the first-token latency for a streamed LLM call.
func RecordLLMFirstToken(model, provider string, duration time.Duration) {
	llmFirstTokenLatency.WithLabelValues(model, provider).Observe(duration.Seconds())
}

// RecordLLMCacheUsage emits cache-related token counters per LLM call.
// `cacheRead` = tokens served from cache; `cacheCreate` = tokens written to
// cache; `inputAfter` = tokens consumed after the last breakpoint. Zero values
// are no-ops (skipped) so callers can pass through Anthropic Usage fields
// unconditionally. The Grafana cache-hit ratio is computed as:
//
//	llm_cache_read_tokens_total
//	------------------------------------------------------------
//	llm_cache_read_tokens_total
//	  + llm_cache_create_tokens_total
//	  + llm_input_tokens_after_breakpoint_total
//
// SECURITY: callers MUST NOT pass raw message content here; emit only integer
// token counts.
func RecordLLMCacheUsage(model string, cacheRead, cacheCreate, inputAfter int) {
	if cacheRead > 0 {
		llmCacheReadTokens.WithLabelValues(model).Add(float64(cacheRead))
	}
	if cacheCreate > 0 {
		llmCacheCreateTokens.WithLabelValues(model).Add(float64(cacheCreate))
	}
	if inputAfter > 0 {
		llmInputTokensAfterBreakpoint.WithLabelValues(model).Add(float64(inputAfter))
	}
}
