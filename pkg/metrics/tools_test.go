package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRecordToolDispatch_IncrementsCounter(t *testing.T) {
	familiesBefore, _ := prometheus.DefaultGatherer.Gather()
	mfBefore := findMetric(familiesBefore, "tool_dispatch_total")
	var baseLine float64
	if mfBefore != nil {
		if s := findSample(mfBefore, map[string]string{
			"tool": "vk__publish_post", "agent": "vk", "status": "success",
		}); s != nil {
			baseLine = s.GetCounter().GetValue()
		}
	}

	RecordToolDispatch("vk__publish_post", "vk", "success", 300*time.Millisecond)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	mf := findMetric(families, "tool_dispatch_total")
	if mf == nil {
		t.Fatal("tool_dispatch_total not found")
	}

	sample := findSample(mf, map[string]string{
		"tool": "vk__publish_post", "agent": "vk", "status": "success",
	})
	if sample == nil {
		t.Fatal("sample not found")
	}
	if sample.GetCounter().GetValue() <= baseLine {
		t.Errorf("counter should have incremented from %f", baseLine)
	}
}

func TestRecordToolDispatch_RecordsDuration(t *testing.T) {
	RecordToolDispatch("telegram__send_channel_post", "telegram", "success", 1500*time.Millisecond)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	mf := findMetric(families, "tool_dispatch_duration_seconds")
	if mf == nil {
		t.Fatal("tool_dispatch_duration_seconds not found")
	}

	sample := findSample(mf, map[string]string{
		"tool": "telegram__send_channel_post", "agent": "telegram",
	})
	if sample == nil {
		t.Fatal("duration sample not found")
	}
	if sample.GetHistogram().GetSampleCount() == 0 {
		t.Error("expected at least one observation")
	}
	if sample.GetHistogram().GetSampleSum() < 1.5 {
		t.Errorf("expected sum >= 1.5s, got %f", sample.GetHistogram().GetSampleSum())
	}
}

func TestRecordToolDispatch_ErrorStatus(t *testing.T) {
	RecordToolDispatch("yandex_business__get_reviews", "yandex_business", "error", 5*time.Second)

	families, _ := prometheus.DefaultGatherer.Gather()
	mf := findMetric(families, "tool_dispatch_total")
	if mf == nil {
		t.Fatal("tool_dispatch_total not found")
	}

	sample := findSample(mf, map[string]string{
		"tool": "yandex_business__get_reviews", "agent": "yandex_business", "status": "error",
	})
	if sample == nil {
		t.Fatal("error sample not found")
	}
}

func TestRecordToolDispatch_MultipleCalls_Accumulate(t *testing.T) {
	tool := "test__multi_call"
	agent := "test"

	// Record multiple calls
	for i := 0; i < 5; i++ {
		RecordToolDispatch(tool, agent, "success", 100*time.Millisecond)
	}

	families, _ := prometheus.DefaultGatherer.Gather()
	mf := findMetric(families, "tool_dispatch_total")
	if mf == nil {
		t.Fatal("tool_dispatch_total not found")
	}

	sample := findSample(mf, map[string]string{
		"tool": tool, "agent": agent, "status": "success",
	})
	if sample == nil {
		t.Fatal("sample not found")
	}
	if sample.GetCounter().GetValue() < 5 {
		t.Errorf("expected counter >= 5 after 5 calls, got %f", sample.GetCounter().GetValue())
	}
}
