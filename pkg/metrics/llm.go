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
	// `llm_cache_hit_ratio`. Labelled by `model` only — MUST NOT add user,
	// business, or content labels (Phase 24 T-24-01-02 mitigation: label
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
)

// RecordLLMRequest records a completed LLM request.
func RecordLLMRequest(model, provider, status string, duration time.Duration) {
	llmRequestsTotal.WithLabelValues(model, provider, status).Inc()
	llmRequestDuration.WithLabelValues(model, provider).Observe(duration.Seconds())
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
// SECURITY (T-24-01-03): callers MUST NOT pass raw message content here; emit
// only integer token counts.
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
