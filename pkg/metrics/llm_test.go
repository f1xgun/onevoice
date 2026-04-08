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
