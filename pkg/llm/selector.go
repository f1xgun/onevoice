package llm

import (
	"sort"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/metrics"
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
	// Candidates returns the priority-ordered candidate list for `model`
	// under `strategy`. Index 0 is the entry Pick would return; subsequent
	// entries are the siblings the Router walks for the one-shot retry.
	// Empty slice when no enabled+registered provider serves the model.
	Candidates(model string, strategy Strategy) []Candidate
	// Record folds an Outcome back into the Selector's policy state. The
	// entry must be one previously returned by Pick — the default impl
	// uses entry.Provider + entry.Model as its metrics key and also
	// mirrors the new health / latency state onto the entry pointer so
	// the next Pick (and any registry consumer) sees it immediately.
	Record(entry *ModelProviderEntry, outcome Outcome)
}

// Candidate pairs a registry entry with its registered Provider, so
// Router.Chat can iterate the priority-ordered list without re-resolving
// providers from the entry.Provider name on every step.
type Candidate struct {
	Entry    *ModelProviderEntry
	Provider Provider
}

// Outcome is the verdict reported back to Selector.Record after a single
// provider invocation. Two latencies live here for two purposes:
//
//   - Latency: end-of-response duration reported by the provider (set by
//     non-stream Chat after the body arrives; zero for streaming starts
//     because the channel-open instant is not user-perceived). Drives the
//     rolling-window AvgLatencyMs used by Pick(StrategySpeed).
//   - Wall: wall-clock duration spent inside the provider call as seen by
//     the caller (Invoke fills this from start/time.Since). Drives the
//     prometheus per-call latency histogram via metrics.RecordLLMRequest.
//
// Model is the model name the caller requested. Invoke fills it; direct
// Record callers (e.g. integration tests that exercise Selector policy
// without going through Invoke) may leave it empty — in that case the
// defaultSelector skips the prometheus emission so non-Router consumers
// of the seam don't accidentally pollute the histogram.
type Outcome struct {
	Success bool
	Latency time.Duration
	Model   string
	Wall    time.Duration
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

// metricsMaxEntries caps the per-(provider,model) metrics map. With many
// model variants and routing strategies the unique key space is unbounded
// over time; a hard cap with single-pass LRU eviction is sufficient at this
// size (1000 keys = a sub-millisecond linear scan on cold-path eviction,
// no background goroutine to lifecycle). Var (not const) so eviction tests
// can lower it without inserting 1000 fixtures.
var metricsMaxEntries = 1000

// nowFunc is the clock source used for metrics last-touched bookkeeping.
// Overridden by tests to drive deterministic LRU eviction.
var nowFunc = time.Now

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
	totalRequests   int64
	successCount    int64
	failureCount    int64
	avgLatencyMs    int
	lastLatencies   []int64
	healthStatus    string
	lastTouchedUnix int64
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
	cands := s.buildCandidates(model, strategy)
	if len(cands) == 0 {
		return nil, nil, ErrNoProvider
	}
	return cands[0].Entry, cands[0].Provider, nil
}

// Candidates returns the priority-ordered candidate list for `model` under
// `strategy`. Index 0 matches Pick's choice; the remaining entries are
// siblings the Router walks for the one-shot retry. Unhealthy and
// degraded entries appear after healthy ones but are NOT filtered out —
// the retry path can attempt a known-bad sibling if the primary fails,
// which is preferable to surfacing the failure to the caller when the
// sibling might have recovered between health checks.
func (s *defaultSelector) Candidates(model string, strategy Strategy) []Candidate {
	return s.buildCandidates(model, strategy)
}

// buildCandidates is the shared ordering pass driven by both Pick and
// Candidates. Two phases: collect enabled+registered entries, then sort
// stably with healthy entries first and the strategy's tie-break inside
// each health bucket. Registry order is preserved for equal keys so the
// output is deterministic across calls.
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

	sort.SliceStable(out, func(i, j int) bool {
		hi := out[i].Entry.HealthStatus == HealthStatusHealthy
		hj := out[j].Entry.HealthStatus == HealthStatusHealthy
		if hi != hj {
			return hi
		}
		// Within the same health bucket, apply the strategy's tie-break.
		if strategy == StrategyCost {
			ci := avgCost(out[i].Entry)
			cj := avgCost(out[j].Entry)
			if ci != cj {
				return ci < cj
			}
			return false
		}
		// StrategySpeed: cheaper average latency wins; zero AvgLatencyMs
		// (no measurements) ranks last so a fresh entry doesn't win on a
		// phantom "zero is smallest" comparison.
		return betterLatency(out[i].Entry, out[j].Entry)
	})

	return out
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

	// Per-call prometheus emission lives here so the seam owns ALL
	// "what to report about a provider invocation" decisions. Gated on
	// outcome.Model so direct Record callers (integration tests etc.)
	// don't leak empty-label series into the histogram — only Invoke
	// callers (Router + future similar wrappers) emit.
	if outcome.Model != "" {
		status := "success"
		if !outcome.Success {
			status = "error"
		}
		metrics.RecordLLMRequest(outcome.Model, entry.Provider, status, outcome.Wall)
	}
}

// Invoke brackets a single Selector cycle — pick a provider, run fn, record
// the outcome, return — so callers don't have to remember to call Record
// (and don't risk drift between two near-identical Chat / ChatStream
// bookkeeping blocks). The closure shape is shared by the blocking and
// streaming paths:
//
//	entry, resp, err := Invoke(sel, req.Model, req.Strategy,
//	    func(p Provider) (*ChatResponse, time.Duration, error) {
//	        r, err := p.Chat(ctx, req)
//	        if err != nil { return nil, 0, err }
//	        return r, r.Latency, nil
//	    })
//
//	entry, ch, err := Invoke(sel, req.Model, req.Strategy,
//	    func(p Provider) (<-chan StreamChunk, time.Duration, error) {
//	        ch, err := p.ChatStream(ctx, req)
//	        if err != nil { return nil, 0, err }
//	        return ch, 0, nil // channel-open instant not user-perceived
//	    })
//
// fn returns the caller's typed result, the provider-reported latency
// (used by the rolling window — zero means "don't sample me"), and any
// error. Invoke fills outcome.Model + outcome.Wall from the caller-
// supplied model and the observed wall-clock; outcome.Success is set
// from err == nil so fn doesn't have to think about it.
//
// On Pick failure fn is not called and Record is not invoked — the error
// has no provider entry to attribute. The Selector seam owns retry policy,
// not Invoke; if multi-provider retry ever lands it goes on Selector,
// not duplicated across each Invoke call site.
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

// evictOldestMetricLocked drops the single least-recently-touched metrics
// entry. Caller must hold s.mu. Linear scan is fine because the map is
// bounded at metricsMaxEntries (1000) and eviction only runs on the cold
// path of inserting into a full map.
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
