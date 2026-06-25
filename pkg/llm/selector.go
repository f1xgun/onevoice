package llm

import (
	"sort"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// Selector chooses a Provider for a model and folds call outcomes back into
// rolling latency / health state. See docs/pkg/llm.md.
type Selector interface {
	// Pick returns the priority-ordered head candidate for `model` under `strategy`.
	Pick(model string, strategy Strategy) (*ModelProviderEntry, Provider, error)
	// Candidates returns the priority-ordered candidate list for `model` under `strategy`.
	Candidates(model string, strategy Strategy) []Candidate
	// Record folds a call Outcome back into the Selector's policy state.
	Record(entry *ModelProviderEntry, outcome Outcome)
}

// Candidate pairs a registry entry with its registered Provider.
type Candidate struct {
	Entry    *ModelProviderEntry
	Provider Provider
}

// Outcome is the verdict reported back to Selector.Record after a single provider invocation.
// Latency drives the rolling AvgLatencyMs; Wall drives the prometheus per-call histogram.
// See docs/pkg/llm.md.
type Outcome struct {
	Success bool
	Latency time.Duration
	Model   string
	Wall    time.Duration
}

// defaultSelector policy tuning. See docs/pkg/llm.md.
const (
	latencyWindow            = 100 // rolling latency samples kept per provider:model
	healthDegradedRate       = 0.2 // failure rate above which a provider is "degraded"
	healthDownRate           = 0.5 // failure rate above which a provider is "down"
	healthRecoverySuccessMin = 3   // consecutive successes that reset a degraded/down state
)

// metricsMaxEntries caps the per-(provider,model) metrics map. Var (not const)
// so eviction tests can lower it without inserting 1000 fixtures.
var metricsMaxEntries = 1000

// nowFunc is the clock source used for metrics last-touched bookkeeping.
// Overridden by tests to drive deterministic LRU eviction.
var nowFunc = time.Now

// defaultSelector is the production Selector. See docs/pkg/llm.md.
//
// mu guards both the metrics map AND the mutable scoring fields that Record
// mirrors onto each *ModelProviderEntry (HealthStatus / AvgLatencyMs /
// LastCheckedAt). buildCandidates reads those same fields while sorting, so it
// holds an RLock — otherwise a concurrent Record write tears the read.
type defaultSelector struct {
	mu        sync.RWMutex
	registry  *Registry
	providers map[string]Provider
	metrics   map[string]*providerMetrics // key: provider + ":" + model
}

// providerMetrics holds runtime state owned by defaultSelector.
//
// consecutiveSuccesses counts successes since the last failure (reset to 0 on
// any failure); it gates recovery so a degraded/down provider only flips back
// to healthy after healthRecoverySuccessMin successes in a row, not on its
// first success after a lifetime of accumulated successCount.
type providerMetrics struct {
	totalRequests        int64
	successCount         int64
	failureCount         int64
	consecutiveSuccesses int64
	avgLatencyMs         int
	lastLatencies        []int64
	healthStatus         string
	lastTouchedUnix      int64
}

// NewSelector constructs the default Selector. See docs/pkg/llm.md.
func NewSelector(registry *Registry, providers map[string]Provider) Selector {
	return &defaultSelector{
		registry:  registry,
		providers: providers,
		metrics:   make(map[string]*providerMetrics),
	}
}

// Pick returns the priority-ordered head candidate, falling back to an unhealthy
// entry rather than ErrNoProvider when no healthy candidate exists. See docs/pkg/llm.md.
func (s *defaultSelector) Pick(model string, strategy Strategy) (*ModelProviderEntry, Provider, error) {
	cands := s.buildCandidates(model, strategy)
	if len(cands) == 0 {
		return nil, nil, ErrNoProvider
	}
	return cands[0].Entry, cands[0].Provider, nil
}

// Candidates returns the priority-ordered candidate list; unhealthy entries
// are kept at the tail so the Router's retry path can still attempt them.
func (s *defaultSelector) Candidates(model string, strategy Strategy) []Candidate {
	return s.buildCandidates(model, strategy)
}

// buildCandidates is the shared ordering pass driven by both Pick and Candidates.
func (s *defaultSelector) buildCandidates(model string, strategy Strategy) []Candidate {
	entries := s.registry.GetModelProviders(model)
	if len(entries) == 0 {
		return nil
	}

	out := make([]Candidate, 0, len(entries))
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		p, ok := s.providers[e.Provider]
		if !ok {
			continue
		}
		out = append(out, Candidate{Entry: e, Provider: p})
	}
	if len(out) == 0 {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		hi := out[i].Entry.HealthStatus == HealthStatusHealthy
		hj := out[j].Entry.HealthStatus == HealthStatusHealthy
		if hi != hj {
			return hi
		}
		if strategy == StrategyCost {
			ci := avgCost(out[i].Entry)
			cj := avgCost(out[j].Entry)
			if ci != cj {
				return ci < cj
			}
			return false
		}
		return betterLatency(out[i].Entry, out[j].Entry)
	})

	return out
}

// Record folds an Outcome into the rolling metrics and mirrors the new
// HealthStatus / AvgLatencyMs onto the entry pointer. See docs/pkg/llm.md.
func (s *defaultSelector) Record(entry *ModelProviderEntry, outcome Outcome) {
	if entry == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := entry.Provider + ":" + entry.Model
	m, ok := s.metrics[key]
	if !ok {
		if len(s.metrics) >= metricsMaxEntries {
			s.evictOldestMetricLocked()
		}
		m = &providerMetrics{
			healthStatus:  HealthStatusHealthy,
			lastLatencies: make([]int64, 0, latencyWindow),
		}
		s.metrics[key] = m
	}
	m.lastTouchedUnix = nowFunc().UnixNano()

	m.totalRequests++

	if outcome.Success {
		m.successCount++
		m.consecutiveSuccesses++
		if outcome.Latency > 0 {
			ms := outcome.Latency.Milliseconds()
			m.lastLatencies = append(m.lastLatencies, ms)
			if len(m.lastLatencies) > latencyWindow {
				m.lastLatencies = m.lastLatencies[1:]
			}
			var sum int64
			for _, l := range m.lastLatencies {
				sum += l
			}
			m.avgLatencyMs = int(sum / int64(len(m.lastLatencies)))
		}
		if m.healthStatus != HealthStatusHealthy && m.consecutiveSuccesses >= healthRecoverySuccessMin {
			m.healthStatus = HealthStatusHealthy
		}
	} else {
		m.failureCount++
		m.consecutiveSuccesses = 0
		failureRate := float64(m.failureCount) / float64(m.totalRequests)
		switch {
		case failureRate > healthDownRate:
			m.healthStatus = HealthStatusDown
		case failureRate > healthDegradedRate:
			m.healthStatus = HealthStatusDegraded
		default:
			m.healthStatus = HealthStatusHealthy
		}
	}

	entry.HealthStatus = m.healthStatus
	if outcome.Success && outcome.Latency > 0 {
		entry.AvgLatencyMs = m.avgLatencyMs
	}
	entry.LastCheckedAt = time.Now()

	if outcome.Model != "" {
		status := "success"
		if !outcome.Success {
			status = "error"
		}
		metrics.RecordLLMRequest(outcome.Model, entry.Provider, status, outcome.Wall)
	}
}

// Invoke brackets a single Selector cycle — Pick, run fn, Record — so callers
// can't forget Record and Chat / ChatStream bookkeeping cannot drift. See docs/pkg/llm.md.
func Invoke[T any](
	s Selector,
	model string,
	strategy Strategy,
	fn func(p Provider) (T, time.Duration, error),
) (*ModelProviderEntry, T, error) {
	var zero T
	entry, provider, err := s.Pick(model, strategy)
	if err != nil {
		return nil, zero, err
	}
	start := time.Now()
	result, providerLatency, fnErr := fn(provider)
	s.Record(entry, Outcome{
		Success: fnErr == nil,
		Latency: providerLatency,
		Model:   model,
		Wall:    time.Since(start),
	})
	return entry, result, fnErr
}

// avgCost averages the input + output per-1M-token list price for StrategyCost.
func avgCost(e *ModelProviderEntry) float64 {
	const ioPair = 2.0
	return (e.InputCostPer1MTok + e.OutputCostPer1MTok) / ioPair
}

// betterLatency returns true when candidate's rolling AvgLatencyMs beats current's.
// Zero AvgLatencyMs ("no measurements yet") ranks last so a fresh entry doesn't
// win on a phantom "zero is smallest" comparison.
func betterLatency(candidate, current *ModelProviderEntry) bool {
	if candidate.AvgLatencyMs == 0 {
		return false
	}
	if current.AvgLatencyMs == 0 {
		return true
	}
	return candidate.AvgLatencyMs < current.AvgLatencyMs
}

// evictOldestMetricLocked drops the single least-recently-touched metrics entry.
// Linear scan is fine because the map is bounded at metricsMaxEntries and
// eviction only runs on the cold path of inserting into a full map.
// Caller must hold s.mu.
func (s *defaultSelector) evictOldestMetricLocked() {
	var oldestKey string
	var oldestTouched int64
	first := true
	for k, m := range s.metrics {
		if first || m.lastTouchedUnix < oldestTouched {
			oldestKey = k
			oldestTouched = m.lastTouchedUnix
			first = false
		}
	}
	if !first {
		delete(s.metrics, oldestKey)
	}
}
