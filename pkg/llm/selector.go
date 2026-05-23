package llm

import (
	"sync"
	"time"
)

// Selector chooses a Provider for a model based on registry config plus
// runtime policy (rolling latency window, failure-rate → health
// transitions). Router consults Selector once per Chat / ChatStream call
// and feeds the outcome back via Record so the next pick can adapt.
//
// Two reasons the seam exists:
//
//  1. Concentration of policy. Before the seam, picking lived on Router
//     (filter + strategy tie-break + fallback) and the health-rollup that
//     fed it lived on Registry (RecordSuccess / RecordFailure / rolling
//     window). Splitting "which provider?" across two modules made the
//     contract — what fields drive selection — diffuse. Selector owns it.
//
//  2. Test surface. Router tests previously had to build a real Registry
//     plus ModelProviderEntry fixtures even to exercise rate-limit,
//     billing, or error-handling paths that don't care about selection.
//     A fake Selector now answers Pick directly so callers exercise the
//     Router behavior without the registry dance.
//
// Concurrent use: implementations must be safe for concurrent Pick / Record
// from any number of goroutines — the Router fans out across requests.
type Selector interface {
	// Pick chooses a provider for `model` under `strategy`. Returns the
	// entry the choice came from so Router can log billing / metrics
	// without the Selector exposing its registry. Returns ErrNoProvider
	// when no enabled+registered provider exists for the model.
	Pick(model string, strategy Strategy) (*ModelProviderEntry, Provider, error)
	// Record folds an Outcome back into the Selector's policy state. The
	// entry must be one previously returned by Pick — the default impl
	// uses entry.Provider + entry.Model as its metrics key and also
	// mirrors the new health / latency state onto the entry pointer so
	// the next Pick (and any registry consumer) sees it immediately.
	Record(entry *ModelProviderEntry, outcome Outcome)
}

// Outcome is the verdict Router reports for a single invocation. Latency
// is zero for streaming starts: the channel-open latency is not the
// user-perceived latency, and rolling-window updates on a misleading
// value would skew future picks.
type Outcome struct {
	Success bool
	Latency time.Duration
}

// defaultSelector policy tuning. Lives at package scope so future Selector
// implementations can lean on the same conventions or override them with
// their own constants.
const (
	latencyWindow            = 100 // rolling latency samples kept per provider:model
	healthDegradedRate       = 0.2 // failure rate above which a provider is "degraded"
	healthDownRate           = 0.5 // failure rate above which a provider is "down"
	healthRecoverySuccessMin = 3   // consecutive successes that reset a degraded/down state
)

// defaultSelector is the production Selector. It consults Registry for
// entries (config) and owns the rolling-window latency + health-transition
// state in its own metrics map — both used to live on Registry, which
// conflated config with runtime policy.
type defaultSelector struct {
	mu        sync.Mutex
	registry  *Registry
	providers map[string]Provider
	metrics   map[string]*providerMetrics // key: provider + ":" + model
}

// providerMetrics holds runtime state owned by defaultSelector. Unexported
// — callers that need policy state read it off the ModelProviderEntry that
// Pick / Record mirror it onto, not directly through the Selector.
type providerMetrics struct {
	totalRequests int64
	successCount  int64
	failureCount  int64
	avgLatencyMs  int
	lastLatencies []int64
	healthStatus  string
}

// NewSelector constructs the default Selector. Exposed so callers wanting
// to wrap or trace the production policy (e.g. a tracing Selector
// decorator) can compose off it instead of reimplementing the rolling
// window from scratch.
func NewSelector(registry *Registry, providers map[string]Provider) Selector {
	return &defaultSelector{
		registry:  registry,
		providers: providers,
		metrics:   make(map[string]*providerMetrics),
	}
}

// Pick chooses the best healthy, enabled, registered provider for `model`.
// Falls back to the first enabled+registered entry (regardless of health)
// when every healthy provider is unavailable — keeps a single-provider
// outage from permanently deadlocking the Router once the provider
// recovers.
func (s *defaultSelector) Pick(model string, strategy Strategy) (*ModelProviderEntry, Provider, error) {
	entries := s.registry.GetModelProviders(model)
	if len(entries) == 0 {
		return nil, nil, ErrNoProvider
	}

	var best *ModelProviderEntry
	var bestProvider Provider

	for _, e := range entries {
		if e.HealthStatus != HealthStatusHealthy || !e.Enabled {
			continue
		}
		p, ok := s.providers[e.Provider]
		if !ok {
			continue
		}
		if best == nil {
			best = e
			bestProvider = p
			continue
		}
		if strategy == StrategyCost {
			if avgCost(e) < avgCost(best) {
				best = e
				bestProvider = p
			}
		} else {
			if betterLatency(e, best) {
				best = e
				bestProvider = p
			}
		}
	}

	if best != nil {
		return best, bestProvider, nil
	}

	// Fallback: every healthy candidate filtered out. Try the first
	// enabled+registered entry anyway so a recovering provider can serve
	// traffic without an explicit health-reset.
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		p, ok := s.providers[e.Provider]
		if !ok {
			continue
		}
		return e, p, nil
	}
	return nil, nil, ErrNoProvider
}

// Record folds an Outcome into the rolling metrics and mirrors the new
// HealthStatus / AvgLatencyMs onto the entry pointer so the next Pick
// (and any registry consumer reading entries for an admin / status
// endpoint) sees the policy state without consulting the Selector.
func (s *defaultSelector) Record(entry *ModelProviderEntry, outcome Outcome) {
	if entry == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := entry.Provider + ":" + entry.Model
	m, ok := s.metrics[key]
	if !ok {
		m = &providerMetrics{
			healthStatus:  HealthStatusHealthy,
			lastLatencies: make([]int64, 0, latencyWindow),
		}
		s.metrics[key] = m
	}

	m.totalRequests++

	if outcome.Success {
		m.successCount++
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
		// Recovery: enough consecutive successes since the last failure
		// flip a degraded / down provider back to healthy.
		if m.healthStatus != HealthStatusHealthy && m.successCount >= healthRecoverySuccessMin {
			m.healthStatus = HealthStatusHealthy
		}
	} else {
		m.failureCount++
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
}

// avgCost averages the input + output per-1M-token list price. Tie-breaker
// for StrategyCost so a model with cheaper input but pricier output isn't
// blindly preferred over a balanced model — we score on the symmetric
// midpoint.
func avgCost(e *ModelProviderEntry) float64 {
	const ioPair = 2.0
	return (e.InputCostPer1MTok + e.OutputCostPer1MTok) / ioPair
}

// betterLatency returns true when candidate's rolling AvgLatencyMs beats
// current's. Zero AvgLatencyMs means "no measurements yet" and ranks last
// — preferring a provider we have data on over one we don't keeps fresh
// entries from monopolising traffic just because their unknown latency
// happens to compare as "smaller."
func betterLatency(candidate, current *ModelProviderEntry) bool {
	if candidate.AvgLatencyMs == 0 {
		return false
	}
	if current.AvgLatencyMs == 0 {
		return true
	}
	return candidate.AvgLatencyMs < current.AvgLatencyMs
}
