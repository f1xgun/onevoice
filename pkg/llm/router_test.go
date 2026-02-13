package llm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers shared across router tests
// ---------------------------------------------------------------------------

func newTestRegistry(entries ...*llm.ModelProviderEntry) *llm.Registry {
	r := llm.NewRegistry()
	for _, e := range entries {
		r.RegisterModelProvider(e)
	}
	return r
}

func healthyEntry(model, provider string, inputCost, outputCost float64, latencyMs int) *llm.ModelProviderEntry {
	return &llm.ModelProviderEntry{
		Model:              model,
		Provider:           provider,
		InputCostPer1MTok:  inputCost,
		OutputCostPer1MTok: outputCost,
		AvgLatencyMs:       latencyMs,
		HealthStatus:       "healthy",
		Enabled:            true,
	}
}

// stubProvider wraps a canned ChatResponse for test use.
type stubProvider struct {
	name     string
	response *llm.ChatResponse
	err      error
}

func (s *stubProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return s.response, s.err
}
func (s *stubProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}
func (s *stubProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (s *stubProvider) HealthCheck(_ context.Context) error                   { return nil }
func (s *stubProvider) Name() string                                           { return s.name }

func makeStub(name string) *stubProvider {
	return &stubProvider{
		name:     name,
		response: &llm.ChatResponse{Content: "ok from " + name},
	}
}

// fakeRateLimiter satisfies RateLimitChecker for tests.
type fakeRateLimiter struct {
	allowed bool
	err     error
}

func (f *fakeRateLimiter) CheckLimit(_ context.Context, _ uuid.UUID, _ string, _ int) (bool, error) {
	return f.allowed, f.err
}

// NOTE: MockBillingRepository is defined in billing_test.go — not redefined here.

// ---------------------------------------------------------------------------
// Task 1 tests
// ---------------------------------------------------------------------------

func TestNewRouter_NilOptions(t *testing.T) {
	registry := llm.NewRegistry()
	r := llm.NewRouter(registry)
	require.NotNil(t, r)
}

func TestNewRouter_WithOptions(t *testing.T) {
	registry := llm.NewRegistry()
	billing := &MockBillingRepository{}
	commission := llm.CommissionConfig{Mode: "percentage"}

	r := llm.NewRouter(registry,
		llm.WithBilling(billing),
		llm.WithCommission(commission),
	)
	require.NotNil(t, r)
}

func TestRouter_ErrNoProvider_NoEntries(t *testing.T) {
	registry := llm.NewRegistry()
	r := llm.NewRouter(registry)

	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	assert.ErrorIs(t, err, llm.ErrNoProvider)
}

func TestRouter_ErrNoProvider_AllUnhealthy(t *testing.T) {
	entry := &llm.ModelProviderEntry{
		Model:        "gpt-4",
		Provider:     "openai",
		HealthStatus: "down",
		Enabled:      true,
	}
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry)

	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	assert.ErrorIs(t, err, llm.ErrNoProvider)
}

func TestRouter_ErrNoProvider_AllDisabled(t *testing.T) {
	entry := &llm.ModelProviderEntry{
		Model:        "gpt-4",
		Provider:     "openai",
		HealthStatus: "healthy",
		Enabled:      false,
	}
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry)

	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	assert.ErrorIs(t, err, llm.ErrNoProvider)
}

// ---------------------------------------------------------------------------
// Task 2 tests — strategy selection
// ---------------------------------------------------------------------------

func TestRouter_StrategyCost_PicksCheapest(t *testing.T) {
	expensive := healthyEntry("gpt-4", "expensive", 10.0, 30.0, 500)
	cheap := healthyEntry("gpt-4", "cheap", 1.0, 3.0, 800)

	registry := newTestRegistry(expensive, cheap)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("expensive")),
		llm.WithProvider(makeStub("cheap")),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-4",
		Strategy: llm.StrategyCost,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok from cheap", resp.Content)
	assert.Equal(t, "cheap", resp.Provider)
}

func TestRouter_StrategySpeed_PicksFastest(t *testing.T) {
	fast := healthyEntry("gpt-4", "fast", 5.0, 15.0, 200)
	slow := healthyEntry("gpt-4", "slow", 5.0, 15.0, 1500)

	registry := newTestRegistry(fast, slow)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("fast")),
		llm.WithProvider(makeStub("slow")),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-4",
		Strategy: llm.StrategySpeed,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok from fast", resp.Content)
}

func TestRouter_StrategySpeed_ZeroLatencyRankedLast(t *testing.T) {
	nodata := healthyEntry("gpt-4", "nodata", 1.0, 1.0, 0)
	measured := healthyEntry("gpt-4", "measured", 5.0, 5.0, 500)

	registry := newTestRegistry(nodata, measured)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("nodata")),
		llm.WithProvider(makeStub("measured")),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-4",
		Strategy: llm.StrategySpeed,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok from measured", resp.Content)
}

func TestRouter_DefaultStrategy_IsCost(t *testing.T) {
	expensive := healthyEntry("gpt-4", "expensive", 20.0, 40.0, 100)
	cheap := healthyEntry("gpt-4", "cheap", 1.0, 2.0, 2000)

	registry := newTestRegistry(expensive, cheap)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("expensive")),
		llm.WithProvider(makeStub("cheap")),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.NoError(t, err)
	assert.Equal(t, "ok from cheap", resp.Content)
}

func TestRouter_SkipsUnregisteredProviders(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry)

	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	assert.ErrorIs(t, err, llm.ErrNoProvider)
}

// ---------------------------------------------------------------------------
// Task 3 tests — rate limiting
// ---------------------------------------------------------------------------

func TestRouter_RateLimit_Allowed(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimitChecker(&fakeRateLimiter{allowed: true}),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:  "gpt-4",
		UserID: uuid.New(),
		Tier:   "free",
	})
	require.NoError(t, err)
	assert.Equal(t, "ok from openai", resp.Content)
}

func TestRouter_RateLimit_Blocked(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimitChecker(&fakeRateLimiter{allowed: false}),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:  "gpt-4",
		UserID: uuid.New(),
		Tier:   "free",
	})
	assert.ErrorIs(t, err, llm.ErrRateLimitExceeded)
}

func TestRouter_RateLimit_SkippedForNilUserID(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimitChecker(&fakeRateLimiter{allowed: false}),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:  "gpt-4",
		UserID: uuid.Nil,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestRouter_RateLimit_CheckerError_Propagated(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimitChecker(&fakeRateLimiter{err: errors.New("redis down")}),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:  "gpt-4",
		UserID: uuid.New(),
	})
	assert.Error(t, err)
	assert.NotErrorIs(t, err, llm.ErrRateLimitExceeded)
}

// ---------------------------------------------------------------------------
// Task 4 tests — billing and failure recording
// ---------------------------------------------------------------------------

func TestRouter_Billing_LoggedAfterSuccess(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 1.0, 3.0, 300)
	registry := newTestRegistry(entry)

	billing := &MockBillingRepository{}
	userID := uuid.New()

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name: "openai",
			response: &llm.ChatResponse{
				Content: "hello",
				Usage:   llm.TokenUsage{InputTokens: 500, OutputTokens: 200, TotalTokens: 700},
			},
		}),
		llm.WithBilling(billing),
		llm.WithCommission(llm.CommissionConfig{Mode: "percentage"}),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:  "gpt-4",
		UserID: userID,
		Tier:   "basic",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Content)

	year, month := time.Now().Year(), int(time.Now().Month())
	logs, err := billing.GetMonthlyUsage(context.Background(), userID, year, month)
	require.NoError(t, err)
	require.Len(t, logs, 1)

	log := logs[0]
	assert.Equal(t, "gpt-4", log.Model)
	assert.Equal(t, "openai", log.Provider)
	assert.Equal(t, 500, log.InputTokens)
	assert.Equal(t, 200, log.OutputTokens)
	assert.Equal(t, "basic", log.UserTier)

	assert.InDelta(t, 0.0011, log.ProviderCostUSD, 1e-9)
	assert.InDelta(t, 0.00022, log.CommissionUSD, 1e-9)
	assert.InDelta(t, 0.00132, log.UserCostUSD, 1e-9)
}

func TestRouter_Billing_NotCalledWhenNil(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 1.0, 3.0, 300)
	registry := newTestRegistry(entry)

	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestRouter_FailureRecorded_WhenProviderErrors(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name: "openai",
			err:  errors.New("provider timeout"),
		}),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	assert.Error(t, err)
	assert.NotErrorIs(t, err, llm.ErrNoProvider)

	for i := 0; i < 6; i++ {
		registry.RecordFailure("openai", "gpt-4")
	}
	providers := registry.GetModelProviders("gpt-4")
	assert.Equal(t, "down", providers[0].HealthStatus)
}

func TestRouter_SuccessRecorded_UpdatesLatency(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 0)
	registry := newTestRegistry(entry)

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name:     "openai",
			response: &llm.ChatResponse{Content: "ok", Latency: 400 * time.Millisecond},
		}),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.NoError(t, err)

	providers := registry.GetModelProviders("gpt-4")
	assert.Equal(t, 400, providers[0].AvgLatencyMs)
}
