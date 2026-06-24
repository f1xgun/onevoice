package llm_test

import (
	"sync"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// TestSelector_ConcurrentPickAndRecord_NoRace exercises the lock discipline
// between the read-only ordering pass (Pick / Candidates -> buildCandidates,
// which reads each entry's HealthStatus / AvgLatencyMs while sorting) and
// Record (which mirrors those same fields onto the entry pointer under the
// selector mutex). Run with -race: an unsynchronized read/write of the entry
// fields trips the detector.
func TestSelector_ConcurrentPickAndRecord_NoRace(t *testing.T) {
	entries := []*llm.ModelProviderEntry{
		healthyEntry("gpt-4", "alpha", 1.0, 1.0, 200),
		healthyEntry("gpt-4", "beta", 2.0, 2.0, 400),
	}
	s, _, _ := newSelectorFixture(entries, "alpha", "beta")

	const goroutines = 8
	const iterations = 500

	var wg sync.WaitGroup
	start := make(chan struct{})

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				switch id % 4 {
				case 0:
					_, _, _ = s.Pick("gpt-4", llm.StrategySpeed)
				case 1:
					_ = s.Candidates("gpt-4", llm.StrategyCost)
				case 2:
					s.Record(entries[0], llm.Outcome{
						Success: i%2 == 0,
						Latency: time.Duration(i) * time.Millisecond,
						Model:   "gpt-4",
					})
				case 3:
					s.Record(entries[1], llm.Outcome{
						Success: i%3 == 0,
						Latency: time.Duration(i*2) * time.Millisecond,
						Model:   "gpt-4",
					})
				}
			}
		}(g)
	}

	close(start)
	wg.Wait()
}
