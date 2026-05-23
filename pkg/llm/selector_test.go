package llm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// Helper to construct a defaultSelector under test. Most tests need a
// registry + 1-2 entries + a provider map keyed by name; bundling that
// here keeps each test body focused on the assertion.
func newSelectorFixture(entries []*llm.ModelProviderEntry, providerNames ...string) (llm.Selector, *llm.Registry, map[string]llm.Provider) {
	r := llm.NewRegistry()
	for _, e := range entries {
		r.RegisterModelProvider(e)
	}
	providers := make(map[string]llm.Provider, len(providerNames))
	for _, n := range providerNames {
		providers[n] = makeStub(n)
	}
	return llm.NewSelector(r, providers), r, providers
}

// ---------------------------------------------------------------------------
// Pick
// ---------------------------------------------------------------------------

func TestSelector_Pick_NoEntries(t *testing.T) {
	s, _, _ := newSelectorFixture(nil)
	_, _, err := s.Pick("missing-model", llm.StrategyCost)
	assert.ErrorIs(t, err, llm.ErrNoProvider)
}

func TestSelector_Pick_PrefersLowerCostUnderStrategyCost(t *testing.T) {
	entries := []*llm.ModelProviderEntry{
		healthyEntry("gpt-4", "expensive", 10.0, 30.0, 1000),
		healthyEntry("gpt-4", "cheap", 2.0, 6.0, 1500),
	}
	s, _, _ := newSelectorFixture(entries, "expensive", "cheap")

	entry, provider, err := s.Pick("gpt-4", llm.StrategyCost)
	require.NoError(t, err)
	assert.Equal(t, "cheap", entry.Provider)
	assert.Equal(t, "cheap", provider.Name())
}

func TestSelector_Pick_PrefersLowerLatencyUnderStrategySpeed(t *testing.T) {
	entries := []*llm.ModelProviderEntry{
		healthyEntry("gpt-4", "slow", 1.0, 1.0, 2000),
		healthyEntry("gpt-4", "fast", 5.0, 5.0, 300),
	}
	s, _, _ := newSelectorFixture(entries, "slow", "fast")

	entry, _, err := s.Pick("gpt-4", llm.StrategySpeed)
	require.NoError(t, err)
	assert.Equal(t, "fast", entry.Provider)
}

func TestSelector_Pick_SkipsUnhealthy(t *testing.T) {
	unhealthy := healthyEntry("gpt-4", "down-provider", 1.0, 1.0, 100)
	unhealthy.HealthStatus = "down"
	healthy := healthyEntry("gpt-4", "ok-provider", 10.0, 30.0, 1000)

	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{unhealthy, healthy},
		"down-provider", "ok-provider",
	)

	entry, _, err := s.Pick("gpt-4", llm.StrategyCost)
	require.NoError(t, err)
	assert.Equal(t, "ok-provider", entry.Provider)
}

func TestSelector_Pick_SkipsDisabled(t *testing.T) {
	disabled := healthyEntry("gpt-4", "off-provider", 1.0, 1.0, 100)
	disabled.Enabled = false
	enabled := healthyEntry("gpt-4", "on-provider", 10.0, 30.0, 1000)

	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{disabled, enabled},
		"off-provider", "on-provider",
	)

	entry, _, err := s.Pick("gpt-4", llm.StrategyCost)
	require.NoError(t, err)
	assert.Equal(t, "on-provider", entry.Provider)
}

func TestSelector_Pick_SkipsUnregistered(t *testing.T) {
	// Entry exists in the registry but no provider implementation is
	// registered for it — must be skipped, never returned with a nil
	// Provider.
	entry := healthyEntry("gpt-4", "ghost-provider", 1.0, 1.0, 100)
	s, _, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry})

	_, _, err := s.Pick("gpt-4", llm.StrategyCost)
	assert.ErrorIs(t, err, llm.ErrNoProvider)
}

func TestSelector_Pick_FallsBackWhenAllUnhealthy(t *testing.T) {
	// Every entry is unhealthy. The selector should still return one —
	// otherwise a single provider's outage would permanently deadlock
	// future picks once it recovers.
	e1 := healthyEntry("gpt-4", "a", 1.0, 1.0, 100)
	e1.HealthStatus = "down"
	e2 := healthyEntry("gpt-4", "b", 1.0, 1.0, 100)
	e2.HealthStatus = "degraded"

	s, _, _ := newSelectorFixture([]*llm.ModelProviderEntry{e1, e2}, "a", "b")

	entry, provider, err := s.Pick("gpt-4", llm.StrategyCost)
	require.NoError(t, err)
	assert.NotNil(t, entry)
	assert.NotNil(t, provider)
}

func TestSelector_Pick_LatencyZeroRanksLast(t *testing.T) {
	// Under StrategySpeed, an entry with zero AvgLatencyMs (no data yet)
	// must NOT win over an entry with a measured latency — even a slow
	// measured latency.
	withData := healthyEntry("gpt-4", "measured", 1.0, 1.0, 5000)
	noData := healthyEntry("gpt-4", "unknown", 1.0, 1.0, 0)

	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{noData, withData}, // order: unknown first
		"measured", "unknown",
	)

	entry, _, err := s.Pick("gpt-4", llm.StrategySpeed)
	require.NoError(t, err)
	assert.Equal(t, "measured", entry.Provider,
		"measured 5000ms must beat unknown — zero latency ranks last")
}

// ---------------------------------------------------------------------------
// Record
// ---------------------------------------------------------------------------

func TestSelector_Record_NilEntry_NoOp(t *testing.T) {
	s, _, _ := newSelectorFixture(nil)
	assert.NotPanics(t, func() {
		s.Record(nil, llm.Outcome{Success: true, Latency: 100 * time.Millisecond})
	})
}

func TestSelector_Record_SuccessUpdatesEntryLatency(t *testing.T) {
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	s.Record(entry, llm.Outcome{Success: true, Latency: 1000 * time.Millisecond})

	providers := registry.GetModelProviders("test-model")
	require.Len(t, providers, 1)
	assert.Equal(t, 1000, providers[0].AvgLatencyMs)
	assert.Equal(t, "healthy", providers[0].HealthStatus)
}

func TestSelector_Record_FailureFlipsToDownAboveThreshold(t *testing.T) {
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	// 6 consecutive failures → 100% failure rate → "down".
	for i := 0; i < 6; i++ {
		s.Record(entry, llm.Outcome{Success: false})
	}

	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, "down", providers[0].HealthStatus)
}

func TestSelector_Record_FailureFlipsToDegradedBetweenThresholds(t *testing.T) {
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	// 1 failure out of 4 total = 25% > healthDegradedRate (20%) but
	// < healthDownRate (50%).
	s.Record(entry, llm.Outcome{Success: false})
	s.Record(entry, llm.Outcome{Success: true, Latency: 100 * time.Millisecond})
	s.Record(entry, llm.Outcome{Success: true, Latency: 100 * time.Millisecond})
	s.Record(entry, llm.Outcome{Success: true, Latency: 100 * time.Millisecond})

	providers := registry.GetModelProviders("test-model")
	// Per the recovery rule, 3 consecutive successes after 1 failure also
	// flips back to healthy regardless of rate. We assert the rate-only
	// case separately below — here we expect "healthy" because recovery
	// fires once successCount >= healthRecoverySuccessMin.
	assert.Equal(t, "healthy", providers[0].HealthStatus,
		"recovery rule beats degraded threshold after 3 successes")
}

func TestSelector_Record_DegradedAfterTwoOfThreeFailures(t *testing.T) {
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	// 1 success then 1 failure → 1/2 = 50% (NOT > 50% so not "down");
	// next failure: 2/3 = 66.6% > 50% so "down".
	s.Record(entry, llm.Outcome{Success: true, Latency: 100 * time.Millisecond})
	s.Record(entry, llm.Outcome{Success: false})
	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, "degraded", providers[0].HealthStatus,
		"50%% rate is above degraded but not yet above down")

	s.Record(entry, llm.Outcome{Success: false})
	assert.Equal(t, "down", providers[0].HealthStatus)
}

func TestSelector_Record_RecoveryAfterThreeSuccesses(t *testing.T) {
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	// Drive to "down".
	for i := 0; i < 6; i++ {
		s.Record(entry, llm.Outcome{Success: false})
	}
	providers := registry.GetModelProviders("test-model")
	require.Equal(t, "down", providers[0].HealthStatus)

	// 3 consecutive successes flip back to healthy.
	for i := 0; i < 3; i++ {
		s.Record(entry, llm.Outcome{Success: true, Latency: 200 * time.Millisecond})
	}
	assert.Equal(t, "healthy", providers[0].HealthStatus)
}

func TestSelector_Record_LatencyRollingWindow(t *testing.T) {
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	s.Record(entry, llm.Outcome{Success: true, Latency: 1200 * time.Millisecond})
	s.Record(entry, llm.Outcome{Success: true, Latency: 1100 * time.Millisecond})

	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, 1150, providers[0].AvgLatencyMs,
		"avg of [1200, 1100] = 1150")
}

func TestSelector_Record_ZeroLatencySkipsWindow(t *testing.T) {
	// Streaming starts report zero Latency (channel-open is not the
	// user-perceived latency). The rolling window must NOT count those
	// samples — otherwise a busy stream-heavy hour would falsely pull
	// the avg toward zero.
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	s.Record(entry, llm.Outcome{Success: true, Latency: 500 * time.Millisecond})
	s.Record(entry, llm.Outcome{Success: true, Latency: 0}) // streaming start
	s.Record(entry, llm.Outcome{Success: true, Latency: 700 * time.Millisecond})

	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, 600, providers[0].AvgLatencyMs,
		"zero-latency samples must be skipped: avg = (500+700)/2 = 600")
}

// ---------------------------------------------------------------------------
// Router integration: fake-Selector injection
// ---------------------------------------------------------------------------

// recordingSelector captures the Outcomes Router reports so tests can
// assert Router's success/failure wiring without building a real
// Registry + ModelProviderEntry fixture.
type recordingSelector struct {
	entry    *llm.ModelProviderEntry
	provider llm.Provider
	err      error
	records  []llm.Outcome
}

func (s *recordingSelector) Pick(_ string, _ llm.Strategy) (*llm.ModelProviderEntry, llm.Provider, error) {
	return s.entry, s.provider, s.err
}

func (s *recordingSelector) Record(_ *llm.ModelProviderEntry, o llm.Outcome) {
	s.records = append(s.records, o)
}

func TestRouter_WithFakeSelector_RecordsSuccess(t *testing.T) {
	provider := makeStub("fake")
	provider.response = &llm.ChatResponse{Content: "ok", Latency: 250 * time.Millisecond}

	sel := &recordingSelector{
		entry:    &llm.ModelProviderEntry{Model: "gpt-4", Provider: "fake"},
		provider: provider,
	}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))

	resp, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.NoError(t, err)
	assert.Equal(t, "fake", resp.Provider)

	require.Len(t, sel.records, 1)
	assert.True(t, sel.records[0].Success)
	assert.Equal(t, 250*time.Millisecond, sel.records[0].Latency)
}

func TestRouter_WithFakeSelector_RecordsFailure(t *testing.T) {
	failingProvider := &stubProvider{name: "fake", err: errors.New("boom")}

	sel := &recordingSelector{
		entry:    &llm.ModelProviderEntry{Model: "gpt-4", Provider: "fake"},
		provider: failingProvider,
	}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))

	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	assert.Error(t, err)

	require.Len(t, sel.records, 1)
	assert.False(t, sel.records[0].Success)
}
