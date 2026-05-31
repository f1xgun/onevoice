package llm

import (
	"strconv"
	"testing"
	"time"
)

// Drives the LRU eviction path of defaultSelector.metrics without standing
// up a real Registry — we hand-build the selector so we can inspect the
// internal map and override nowFunc deterministically.
func TestDefaultSelector_MetricsEvictsOldestWhenAtCap(t *testing.T) {
	origNow := nowFunc
	origMax := metricsMaxEntries
	t.Cleanup(func() {
		nowFunc = origNow
		metricsMaxEntries = origMax
	})
	metricsMaxEntries = 3

	s := &defaultSelector{
		registry:  NewRegistry(),
		providers: map[string]Provider{},
		metrics:   make(map[string]*providerMetrics),
	}

	base := time.Unix(1_700_000_000, 0)
	clock := base
	nowFunc = func() time.Time { return clock }

	keys := []string{"a", "b", "c"}
	for i, k := range keys {
		clock = base.Add(time.Duration(i) * time.Second)
		s.Record(&ModelProviderEntry{Provider: k, Model: "m"}, Outcome{Success: true})
	}

	if len(s.metrics) != 3 {
		t.Fatalf("expected 3 entries at cap, got %d", len(s.metrics))
	}

	// Touch "a" so "b" becomes the oldest by lastTouchedUnix.
	clock = base.Add(10 * time.Second)
	s.Record(&ModelProviderEntry{Provider: "a", Model: "m"}, Outcome{Success: true})

	// Insert a fourth → forces eviction of the LRU entry ("b:m").
	clock = base.Add(11 * time.Second)
	s.Record(&ModelProviderEntry{Provider: "d", Model: "m"}, Outcome{Success: true})

	if len(s.metrics) != 3 {
		t.Fatalf("expected map to stay at cap, got %d", len(s.metrics))
	}
	if _, ok := s.metrics["b:m"]; ok {
		t.Fatalf("expected oldest entry b:m to be evicted; map: %v", mapKeysForTest(s.metrics))
	}
	for _, want := range []string{"a:m", "c:m", "d:m"} {
		if _, ok := s.metrics[want]; !ok {
			t.Fatalf("expected %s to remain; map: %v", want, mapKeysForTest(s.metrics))
		}
	}
}

func TestDefaultSelector_MetricsUnderCapDoesNotEvict(t *testing.T) {
	origMax := metricsMaxEntries
	t.Cleanup(func() { metricsMaxEntries = origMax })
	metricsMaxEntries = 5

	s := &defaultSelector{
		registry:  NewRegistry(),
		providers: map[string]Provider{},
		metrics:   make(map[string]*providerMetrics),
	}
	for i := 0; i < 4; i++ {
		s.Record(&ModelProviderEntry{Provider: "p" + strconv.Itoa(i), Model: "m"}, Outcome{Success: true})
	}
	if len(s.metrics) != 4 {
		t.Fatalf("expected 4 entries below cap, got %d", len(s.metrics))
	}
}

func mapKeysForTest(m map[string]*providerMetrics) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
