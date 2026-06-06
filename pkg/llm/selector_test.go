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
	entry := healthyEntry("gpt-4", "ghost-provider", 1.0, 1.0, 100)
	s, _, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry})

	_, _, err := s.Pick("gpt-4", llm.StrategyCost)
	assert.ErrorIs(t, err, llm.ErrNoProvider)
}

func TestSelector_Pick_FallsBackWhenAllUnhealthy(t *testing.T) {
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
	withData := healthyEntry("gpt-4", "measured", 1.0, 1.0, 5000)
	noData := healthyEntry("gpt-4", "unknown", 1.0, 1.0, 0)

	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{noData, withData},
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

	for i := 0; i < 6; i++ {
		s.Record(entry, llm.Outcome{Success: false})
	}

	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, "down", providers[0].HealthStatus)
}

func TestSelector_Record_FailureFlipsToDegradedBetweenThresholds(t *testing.T) {
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	s.Record(entry, llm.Outcome{Success: false})
	s.Record(entry, llm.Outcome{Success: true, Latency: 100 * time.Millisecond})
	s.Record(entry, llm.Outcome{Success: true, Latency: 100 * time.Millisecond})
	s.Record(entry, llm.Outcome{Success: true, Latency: 100 * time.Millisecond})

	providers := registry.GetModelProviders("test-model")
	assert.Equal(t, "healthy", providers[0].HealthStatus,
		"recovery rule beats degraded threshold after 3 successes")
}

func TestSelector_Record_DegradedAfterTwoOfThreeFailures(t *testing.T) {
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

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

	for i := 0; i < 6; i++ {
		s.Record(entry, llm.Outcome{Success: false})
	}
	providers := registry.GetModelProviders("test-model")
	require.Equal(t, "down", providers[0].HealthStatus)

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
	entry := healthyEntry("test-model", "test-provider", 0, 0, 0)
	s, registry, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "test-provider")

	s.Record(entry, llm.Outcome{Success: true, Latency: 500 * time.Millisecond})
	s.Record(entry, llm.Outcome{Success: true, Latency: 0})
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

func (s *recordingSelector) Candidates(_ string, _ llm.Strategy) []llm.Candidate {
	if s.err != nil || s.entry == nil {
		return nil
	}
	return []llm.Candidate{{Entry: s.entry, Provider: s.provider}}
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

// ---------------------------------------------------------------------------
// Invoke[T]: bracketed pick → run → record
// ---------------------------------------------------------------------------

func TestInvoke_Success_RecordsOutcomeAndReturnsResult(t *testing.T) {
	sel := &recordingSelector{
		entry:    &llm.ModelProviderEntry{Model: "gpt-4", Provider: "fake"},
		provider: makeStub("fake"),
	}

	entry, result, err := llm.Invoke(sel, "gpt-4", llm.StrategyCost,
		func(p llm.Provider) (string, time.Duration, error) {
			return "provider:" + p.Name(), 175 * time.Millisecond, nil
		})
	require.NoError(t, err)
	assert.Equal(t, "provider:fake", result)
	assert.Equal(t, "fake", entry.Provider)

	require.Len(t, sel.records, 1)
	assert.True(t, sel.records[0].Success)
	assert.Equal(t, "gpt-4", sel.records[0].Model,
		"Invoke must fill outcome.Model from the requested model")
	assert.Equal(t, 175*time.Millisecond, sel.records[0].Latency,
		"Invoke must propagate provider-reported latency into outcome.Latency")
}

func TestInvoke_ProviderError_RecordsFailureAndPropagatesError(t *testing.T) {
	sel := &recordingSelector{
		entry:    &llm.ModelProviderEntry{Model: "gpt-4", Provider: "fake"},
		provider: makeStub("fake"),
	}
	wantErr := errors.New("provider boom")

	entry, result, err := llm.Invoke(sel, "gpt-4", llm.StrategyCost,
		func(_ llm.Provider) (string, time.Duration, error) {
			return "", 0, wantErr
		})
	assert.ErrorIs(t, err, wantErr)
	assert.Empty(t, result, "Invoke must return zero T when fn errors")
	assert.NotNil(t, entry, "Invoke must still return the picked entry on fn error so the caller can attribute the failure")

	require.Len(t, sel.records, 1)
	assert.False(t, sel.records[0].Success,
		"outcome.Success must be false when fn returns a non-nil error")
}

func TestInvoke_PickError_DoesNotRunFnNorRecord(t *testing.T) {
	pickErr := errors.New("no provider for model")
	sel := &recordingSelector{err: pickErr}
	fnCalled := false

	entry, _, err := llm.Invoke(sel, "gpt-4", llm.StrategyCost,
		func(_ llm.Provider) (string, time.Duration, error) {
			fnCalled = true
			return "should-not-happen", 0, nil
		})
	assert.ErrorIs(t, err, pickErr)
	assert.Nil(t, entry, "no entry on pick failure — nothing to attribute")
	assert.False(t, fnCalled, "fn must not run when Pick fails")
	assert.Empty(t, sel.records, "Record must not be called when Pick fails")
}

func TestInvoke_WallClock_PopulatedFromObservedDuration(t *testing.T) {
	const minSleep = 5 * time.Millisecond
	sel := &recordingSelector{
		entry:    &llm.ModelProviderEntry{Model: "gpt-4", Provider: "fake"},
		provider: makeStub("fake"),
	}

	_, _, err := llm.Invoke(sel, "gpt-4", llm.StrategyCost,
		func(_ llm.Provider) (struct{}, time.Duration, error) {
			time.Sleep(minSleep)
			return struct{}{}, 0, nil
		})
	require.NoError(t, err)
	require.Len(t, sel.records, 1)
	assert.GreaterOrEqual(t, sel.records[0].Wall, minSleep,
		"Invoke must fill outcome.Wall with the observed fn duration")
}

func TestInvoke_GenericResultType_Works(t *testing.T) {
	sel := &recordingSelector{
		entry:    &llm.ModelProviderEntry{Model: "gpt-4", Provider: "fake"},
		provider: makeStub("fake"),
	}

	_, ch, err := llm.Invoke(sel, "gpt-4", llm.StrategyCost,
		func(_ llm.Provider) (<-chan llm.StreamChunk, time.Duration, error) {
			out := make(chan llm.StreamChunk)
			close(out)
			return out, 0, nil
		})
	require.NoError(t, err)
	require.NotNil(t, ch)

	require.Len(t, sel.records, 1)
	assert.True(t, sel.records[0].Success)
	assert.Equal(t, time.Duration(0), sel.records[0].Latency,
		"streaming path reports zero Latency so the rolling window skips the sample")
}

// ---------------------------------------------------------------------------
// Candidates: priority-ordered candidate list for retry-once
// ---------------------------------------------------------------------------

func TestSelector_Candidates_SinglyConfigured(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 1.0, 3.0, 100)
	s, _, _ := newSelectorFixture([]*llm.ModelProviderEntry{entry}, "openai")

	cands := s.Candidates("gpt-4", llm.StrategyCost)
	require.Len(t, cands, 1)
	assert.Equal(t, "openai", cands[0].Entry.Provider)
	assert.NotNil(t, cands[0].Provider)
}

func TestSelector_Candidates_MultiplyConfigured(t *testing.T) {
	expensive := healthyEntry("gpt-4", "expensive", 10.0, 30.0, 1000)
	cheap := healthyEntry("gpt-4", "cheap", 2.0, 6.0, 1500)
	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{expensive, cheap}, "expensive", "cheap",
	)

	cands := s.Candidates("gpt-4", llm.StrategyCost)
	require.Len(t, cands, 2)
	assert.Equal(t, "cheap", cands[0].Entry.Provider,
		"index 0 must match Pick's choice under StrategyCost")
	assert.Equal(t, "expensive", cands[1].Entry.Provider,
		"the sibling is the runner-up")
}

func TestSelector_Candidates_NoEntries(t *testing.T) {
	s, _, _ := newSelectorFixture(nil)
	cands := s.Candidates("missing-model", llm.StrategyCost)
	assert.Empty(t, cands, "unknown model returns empty slice, not an error")
}

func TestSelector_Candidates_HealthBucketRollup(t *testing.T) {
	healthy := healthyEntry("gpt-4", "ok", 5.0, 15.0, 500)
	down := healthyEntry("gpt-4", "broken", 1.0, 1.0, 100)
	down.HealthStatus = "down"

	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{down, healthy}, "broken", "ok",
	)

	cands := s.Candidates("gpt-4", llm.StrategyCost)
	require.Len(t, cands, 2,
		"unhealthy entries stay in the list so the retry path can attempt them")
	assert.Equal(t, "ok", cands[0].Entry.Provider,
		"healthy bucket sorts before unhealthy")
	assert.Equal(t, "broken", cands[1].Entry.Provider)
}

func TestSelector_Candidates_StrategyAware(t *testing.T) {
	fast := healthyEntry("gpt-4", "fast", 5.0, 15.0, 200)
	slow := healthyEntry("gpt-4", "slow", 1.0, 3.0, 1500)

	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{fast, slow}, "fast", "slow",
	)

	costOrder := s.Candidates("gpt-4", llm.StrategyCost)
	require.Len(t, costOrder, 2)
	assert.Equal(t, "slow", costOrder[0].Entry.Provider,
		"StrategyCost ranks cheaper input/output first")

	speedOrder := s.Candidates("gpt-4", llm.StrategySpeed)
	require.Len(t, speedOrder, 2)
	assert.Equal(t, "fast", speedOrder[0].Entry.Provider,
		"StrategySpeed ranks lower AvgLatencyMs first")
}

func TestSelector_Candidates_SkipsDisabledAndUnregistered(t *testing.T) {
	disabled := healthyEntry("gpt-4", "off", 1.0, 1.0, 100)
	disabled.Enabled = false
	ghost := healthyEntry("gpt-4", "ghost", 1.0, 1.0, 100)
	enabled := healthyEntry("gpt-4", "ok", 5.0, 15.0, 500)

	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{disabled, ghost, enabled},
		"off", "ok",
	)

	cands := s.Candidates("gpt-4", llm.StrategyCost)
	require.Len(t, cands, 1,
		"disabled and unregistered entries are excluded")
	assert.Equal(t, "ok", cands[0].Entry.Provider)
}

func TestSelector_Candidates_DeterministicOrdering(t *testing.T) {
	a := healthyEntry("gpt-4", "alpha", 1.0, 1.0, 100)
	b := healthyEntry("gpt-4", "beta", 1.0, 1.0, 100)
	s, _, _ := newSelectorFixture(
		[]*llm.ModelProviderEntry{a, b}, "alpha", "beta",
	)

	first := s.Candidates("gpt-4", llm.StrategyCost)
	require.Len(t, first, 2)
	for i := 0; i < 5; i++ {
		next := s.Candidates("gpt-4", llm.StrategyCost)
		require.Len(t, next, 2)
		assert.Equal(t, first[0].Entry.Provider, next[0].Entry.Provider)
		assert.Equal(t, first[1].Entry.Provider, next[1].Entry.Provider)
	}
}
