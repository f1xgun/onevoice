package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRecordLLMRequest_IncrementsCounter(t *testing.T) {
	// Get baseline
	familiesBefore, _ := prometheus.DefaultGatherer.Gather()
	mfBefore := findMetric(familiesBefore, "llm_requests_total")
	var baseLine float64
	if mfBefore != nil {
		if s := findSample(mfBefore, map[string]string{
			"model": "gpt-4", "provider": "openai", "status": "success",
		}); s != nil {
			baseLine = s.GetCounter().GetValue()
		}
	}

	RecordLLMRequest("gpt-4", "openai", "success", 500*time.Millisecond)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	mf := findMetric(families, "llm_requests_total")
	if mf == nil {
		t.Fatal("llm_requests_total not found")
	}

	sample := findSample(mf, map[string]string{
		"model": "gpt-4", "provider": "openai", "status": "success",
	})
	if sample == nil {
		t.Fatal("sample not found")
	}
	if sample.GetCounter().GetValue() <= baseLine {
		t.Errorf("counter should have incremented from %f", baseLine)
	}
}

func TestRecordLLMRequest_RecordsDuration(t *testing.T) {
	RecordLLMRequest("claude-3", "anthropic", "success", 2*time.Second)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	mf := findMetric(families, "llm_request_duration_seconds")
	if mf == nil {
		t.Fatal("llm_request_duration_seconds not found")
	}

	sample := findSample(mf, map[string]string{
		"model": "claude-3", "provider": "anthropic",
	})
	if sample == nil {
		t.Fatal("duration sample not found")
	}
	if sample.GetHistogram().GetSampleCount() == 0 {
		t.Error("expected at least one observation in histogram")
	}
	if sample.GetHistogram().GetSampleSum() < 2.0 {
		t.Errorf("expected sum >= 2.0s, got %f", sample.GetHistogram().GetSampleSum())
	}
}

func TestRecordLLMRequest_ErrorStatus(t *testing.T) {
	RecordLLMRequest("gpt-4", "openrouter", "error", 100*time.Millisecond)

	families, _ := prometheus.DefaultGatherer.Gather()
	mf := findMetric(families, "llm_requests_total")
	if mf == nil {
		t.Fatal("llm_requests_total not found")
	}

	sample := findSample(mf, map[string]string{
		"model": "gpt-4", "provider": "openrouter", "status": "error",
	})
	if sample == nil {
		t.Fatal("error sample not found — error status should be recorded")
	}
}

// counterValue returns the current value of a counter labeled by model only.
// Returns 0 if the metric family or sample is missing.
func counterValue(t *testing.T, name, model string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	mf := findMetric(families, name)
	if mf == nil {
		return 0
	}
	s := findSample(mf, map[string]string{"model": model})
	if s == nil {
		return 0
	}
	return s.GetCounter().GetValue()
}

func TestRecordLLMCacheUsage_EmitsCounters(t *testing.T) {
	const model = "claude-haiku-4-5-metric-test"

	baseRead := counterValue(t, "llm_cache_read_tokens_total", model)
	baseCreate := counterValue(t, "llm_cache_create_tokens_total", model)
	baseInput := counterValue(t, "llm_input_tokens_after_breakpoint_total", model)

	RecordLLMCacheUsage(model, 100, 50, 200)

	if got := counterValue(t, "llm_cache_read_tokens_total", model); got != baseRead+100 {
		t.Errorf("cache_read: expected +100 from %f, got %f", baseRead, got)
	}
	if got := counterValue(t, "llm_cache_create_tokens_total", model); got != baseCreate+50 {
		t.Errorf("cache_create: expected +50 from %f, got %f", baseCreate, got)
	}
	if got := counterValue(t, "llm_input_tokens_after_breakpoint_total", model); got != baseInput+200 {
		t.Errorf("input_after: expected +200 from %f, got %f", baseInput, got)
	}
}

// TestLLMDailySpendBlocked_Increments — counter compiles and increments
// without panic for the tier label values used by the rate limiter.
func TestLLMDailySpendBlocked_Increments(t *testing.T) {
	for _, tier := range []string{"free", "basic", "pro", "enterprise"} {
		LLMDailySpendBlocked.WithLabelValues(tier).Inc()
	}
}

// TestLLMConversationCapHit_Increments — counter compiles for both axes.
func TestLLMConversationCapHit_Increments(t *testing.T) {
	for _, axis := range []string{"input", "output"} {
		LLMConversationCapHit.WithLabelValues(axis).Inc()
	}
}

// TestLLMRedisDownFallback_Increments — counter accepts all four documented
// action labels without panic.
func TestLLMRedisDownFallback_Increments(t *testing.T) {
	for _, action := range []string{"block", "fallback", "fallback_blocked", "misconfigured"} {
		LLMRedisDownFallback.WithLabelValues(action).Inc()
	}
}

// TestLLMRouterRetry_Increments — counter accepts the cartesian product of the
// result and attempt label vocabulary the router emits.
func TestLLMRouterRetry_Increments(t *testing.T) {
	for _, result := range []string{"success", "retrying", "exhausted", "nonretryable"} {
		for _, attempt := range []string{"first", "second"} {
			LLMRouterRetry.WithLabelValues(result, attempt).Inc()
		}
	}
}

func TestRecordLLMCacheUsage_ZeroArgsAreNoOp(t *testing.T) {
	const model = "claude-haiku-4-5-zero-test"

	baseRead := counterValue(t, "llm_cache_read_tokens_total", model)
	baseCreate := counterValue(t, "llm_cache_create_tokens_total", model)
	baseInput := counterValue(t, "llm_input_tokens_after_breakpoint_total", model)

	// Only cacheCreate is positive; the other two args should be skipped.
	RecordLLMCacheUsage(model, 0, 25, 0)

	if got := counterValue(t, "llm_cache_read_tokens_total", model); got != baseRead {
		t.Errorf("cache_read should not change on zero arg, got %f -> %f", baseRead, got)
	}
	if got := counterValue(t, "llm_cache_create_tokens_total", model); got != baseCreate+25 {
		t.Errorf("cache_create: expected +25 from %f, got %f", baseCreate, got)
	}
	if got := counterValue(t, "llm_input_tokens_after_breakpoint_total", model); got != baseInput {
		t.Errorf("input_after should not change on zero arg, got %f -> %f", baseInput, got)
	}
}
