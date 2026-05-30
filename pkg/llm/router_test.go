package llm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
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
func (s *stubProvider) Name() string                                          { return s.name }

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
	businessID := uuid.New() // Non-nil BusinessID required to bill.

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
		Model:      "gpt-4",
		UserID:     userID,
		BusinessID: businessID,
		Tier:       "basic",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Content)

	year, month := time.Now().Year(), int(time.Now().Month())

	// Wait for async billing goroutine to complete.
	assert.Eventually(t, func() bool {
		logs, err := billing.GetMonthlyUsage(context.Background(), userID, year, month)
		return err == nil && len(logs) == 1
	}, 200*time.Millisecond, 5*time.Millisecond, "billing log should appear")

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

// Chat() with a non-nil BusinessID must produce a UsageLog carrying that
// exact BusinessID. Locks the propagation chain
// ChatRequest.BusinessID → logBilling → UsageLog.BusinessID.
func TestRouter_Billing_PersistsBusinessID(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 1.0, 3.0, 300)
	registry := newTestRegistry(entry)

	billing := &MockBillingRepository{}
	businessID := uuid.New()

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name: "openai",
			response: &llm.ChatResponse{
				Content: "ok",
				Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 5},
			},
		}),
		llm.WithBilling(billing),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "gpt-4",
		UserID:     uuid.New(),
		BusinessID: businessID,
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		return billing.LastLog() != nil
	}, 200*time.Millisecond, 10*time.Millisecond, "billing log should appear")

	last := billing.LastLog()
	require.NotNil(t, last)
	assert.Equal(t, businessID, last.BusinessID)
}

// Chat() with BusinessID == uuid.Nil must NOT call the Writer at all
// (system-level callers titler/draft_reply currently pass uuid.Nil; the
// wiring layer retro-fits them with real BusinessIDs at call sites).
func TestRouter_Billing_SkipsWhenBusinessIDNil(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 1.0, 3.0, 300)
	registry := newTestRegistry(entry)

	billing := &MockBillingRepository{}

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name: "openai",
			response: &llm.ChatResponse{
				Content: "ok",
				Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 5},
			},
		}),
		llm.WithBilling(billing),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "gpt-4",
		UserID:     uuid.New(),
		BusinessID: uuid.Nil, // explicit nil — Router must skip billing
	})
	require.NoError(t, err)

	// Allow time for any (incorrectly fired) goroutine to land.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, billing.CallCount(),
		"LogUsage must NOT be called when ChatRequest.BusinessID is uuid.Nil")
}

// Cache reads are billed at 0.1× the input rate.
// Locks the cache-aware formula to 1e-9 precision.
//
//	billable_input = 1000 + 10000*0.1 = 2000
//	provider_cost  = 2000 * 3 / 1_000_000 = 6.0e-6 USD
//	                + 0 * 15 / 1_000_000 (no output tokens)
func TestRouter_Billing_CacheReadDiscounted(t *testing.T) {
	entry := healthyEntry("anthropic/claude-sonnet-4-6", "anthropic", 3.0, 15.0, 300)
	registry := newTestRegistry(entry)

	billing := &MockBillingRepository{}

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name: "anthropic",
			response: &llm.ChatResponse{
				Content: "cached",
				Usage: llm.TokenUsage{
					InputTokens:     1000,
					OutputTokens:    0,
					CacheReadTokens: 10000,
				},
			},
		}),
		llm.WithBilling(billing),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "anthropic/claude-sonnet-4-6",
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool { return billing.LastLog() != nil },
		200*time.Millisecond, 10*time.Millisecond)

	last := billing.LastLog()
	require.NotNil(t, last)
	const expectedProviderCost = (1000.0 + 10000.0*0.1) * 3.0 / 1_000_000.0
	assert.InDelta(t, expectedProviderCost, last.ProviderCostUSD, 1e-9)
	assert.Equal(t, 10000, last.CacheReadTokens)
	assert.Equal(t, 1000, last.InputTokens)
}

// Cache writes are billed at 1.25× the input rate.
//
//	billable_input = 0 + 10000*1.25 + 0 = 12500
//	provider_cost  = 12500 * 3 / 1_000_000 = 3.75e-5 USD
func TestRouter_Billing_CacheWritesPriced(t *testing.T) {
	entry := healthyEntry("anthropic/claude-sonnet-4-6", "anthropic", 3.0, 15.0, 300)
	registry := newTestRegistry(entry)

	billing := &MockBillingRepository{}

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name: "anthropic",
			response: &llm.ChatResponse{
				Content: "primed",
				Usage: llm.TokenUsage{
					InputTokens:         0,
					OutputTokens:        0,
					CacheCreationTokens: 10000,
				},
			},
		}),
		llm.WithBilling(billing),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "anthropic/claude-sonnet-4-6",
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool { return billing.LastLog() != nil },
		200*time.Millisecond, 10*time.Millisecond)

	last := billing.LastLog()
	require.NotNil(t, last)
	const expectedProviderCost = (0.0 + 10000.0*1.25) * 3.0 / 1_000_000.0
	assert.InDelta(t, expectedProviderCost, last.ProviderCostUSD, 1e-9)
	assert.Equal(t, 10000, last.CacheCreationTokens)
}

// Conversation ID propagates verbatim from ChatRequest into the UsageLog.
// Mongo ObjectID hex strings are passed through as-is.
func TestRouter_Billing_PersistsConversationID(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 1.0, 3.0, 300)
	registry := newTestRegistry(entry)

	billing := &MockBillingRepository{}
	const convoID = "67f4a8b27a9ad15d4f8a1c00"

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name: "openai",
			response: &llm.ChatResponse{
				Content: "ok",
				Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 5},
			},
		}),
		llm.WithBilling(billing),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:          "gpt-4",
		UserID:         uuid.New(),
		BusinessID:     uuid.New(),
		ConversationID: convoID,
		RequestID:      "req-789",
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool { return billing.LastLog() != nil },
		200*time.Millisecond, 10*time.Millisecond)

	last := billing.LastLog()
	require.NotNil(t, last)
	assert.Equal(t, convoID, last.ConversationID)
	assert.Equal(t, "req-789", last.RequestID)
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
	// Drives the Router with a failing provider 6 times — Router must
	// call Selector.Record(Success:false) on each error, which the
	// default Selector accumulates into a failure rate > 50% (down).
	// Pre-refactor this test reached past Router and called
	// registry.RecordFailure manually; the seam now makes the recording
	// path observable end-to-end.
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)

	r := llm.NewRouter(registry,
		llm.WithProvider(&stubProvider{
			name: "openai",
			err:  errors.New("provider timeout"),
		}),
	)

	for i := 0; i < 6; i++ {
		_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
		assert.Error(t, err)
		assert.NotErrorIs(t, err, llm.ErrNoProvider)
	}
	providers := registry.GetModelProviders("gpt-4")
	assert.Equal(t, "down", providers[0].HealthStatus,
		"Router must thread provider errors through to Selector.Record")
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
