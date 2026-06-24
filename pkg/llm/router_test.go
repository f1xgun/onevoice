package llm_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
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

// fakeRateLimiter satisfies RateLimitChecker for tests. Captures the userID
// and businessID it was called with so tests can assert propagation.
type fakeRateLimiter struct {
	allowed bool
	err     error

	gotUserID     uuid.UUID
	gotBusinessID uuid.UUID
	gotTokens     int

	// recordCh receives the reconcile delta. Buffered so the fire-and-forget
	// goroutine never blocks; tests select on it (or a timeout) to observe the
	// async reconcile deterministically.
	recordCh chan int
}

func (f *fakeRateLimiter) CheckLimit(_ context.Context, userID, businessID uuid.UUID, _ string, tokens int) (bool, error) {
	f.gotUserID = userID
	f.gotBusinessID = businessID
	f.gotTokens = tokens
	return f.allowed, f.err
}

// RecordTokens makes fakeRateLimiter satisfy llm.TokenRecorder so the router's
// post-response reconcile path is exercised.
func (f *fakeRateLimiter) RecordTokens(_ context.Context, _, _ uuid.UUID, _ string, deltaTokens int) {
	if f.recordCh != nil {
		f.recordCh <- deltaTokens
	}
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

// TestRouter_CheckRateLimit_PassesBusinessID — Chat with non-nil BusinessID
// reaches CheckLimit with that same UUID.
func TestRouter_CheckRateLimit_PassesBusinessID(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	frl := &fakeRateLimiter{allowed: true}
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimitChecker(frl),
	)

	bizID := uuid.New()
	userID := uuid.New()
	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "gpt-4",
		UserID:     userID,
		BusinessID: bizID,
		Tier:       "free",
	})
	require.NoError(t, err)
	assert.Equal(t, userID, frl.gotUserID)
	assert.Equal(t, bizID, frl.gotBusinessID)
}

// TestRouter_CheckRateLimit_PassesNonZeroEstimate — the router must charge the
// token gate a pre-flight ESTIMATE derived from the prompt, not a literal 0.
// This is the fail-on-revert anchor: restore CheckLimit(..., 0) at router.go and
// this assertion fails because gotTokens drops to 0.
func TestRouter_CheckRateLimit_PassesNonZeroEstimate(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	frl := &fakeRateLimiter{allowed: true}
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimitChecker(frl),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:  "gpt-4",
		UserID: uuid.New(),
		Tier:   "free",
		Messages: []llm.Message{
			{Role: "system", Content: strings.Repeat("x", 4000)},
			{Role: "user", Content: strings.Repeat("y", 4000)},
		},
	})
	require.NoError(t, err)
	assert.Positive(t, frl.gotTokens, "router must pass a non-zero prompt-token estimate to CheckLimit")
	assert.GreaterOrEqual(t, frl.gotTokens, 2000,
		"~8000 prompt chars should estimate ~2000 tokens (chars/4), got %d", frl.gotTokens)
}

// TestRouter_RateLimit_BlocksWhenEstimateOverGate — with a real RateLimiter and
// free TokensPerMin=5000, a single large-context turn whose estimate exceeds the
// gate is rejected; a small turn under the gate is allowed. This proves the
// previously-dead token gate now bites end-to-end through the router. Reverting
// router.go to CheckLimit(..., 0) makes the large turn pass → this test fails.
func TestRouter_RateLimit_BlocksWhenEstimateOverGate(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	limiter := llm.NewRateLimiter(rdb, llm.DefaultTierLimits)
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimiter(limiter),
	)

	smallUser := uuid.New()
	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-4",
		UserID:   smallUser,
		Tier:     "free",
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err, "a tiny prompt is well under TokensPerMin=5000")

	bigUser := uuid.New()
	_, err = r.Chat(context.Background(), llm.ChatRequest{
		Model:  "gpt-4",
		UserID: bigUser,
		Tier:   "free",
		Messages: []llm.Message{
			{Role: "user", Content: strings.Repeat("z", 40000)}, // ~10000 est tokens > 5000
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrRateLimitExceeded,
		"a prompt whose estimate exceeds TokensPerMin must be blocked")
}

// TestRouter_ReconcileTokens_RecordsDelta — after a successful response the
// router tops up the token counter with max(0, actualTotal - estimate) via the
// optional TokenRecorder seam, fire-and-forget.
func TestRouter_ReconcileTokens_RecordsDelta(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	frl := &fakeRateLimiter{allowed: true, recordCh: make(chan int, 1)}

	stub := &stubProvider{
		name: "openai",
		response: &llm.ChatResponse{
			Content: "ok",
			Usage:   llm.TokenUsage{TotalTokens: 9000},
		},
	}
	r := llm.NewRouter(registry,
		llm.WithProvider(stub),
		llm.WithRateLimitChecker(frl),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-4",
		UserID:   uuid.New(),
		Tier:     "free",
		Messages: []llm.Message{{Role: "user", Content: strings.Repeat("a", 4000)}}, // est ~1000
	})
	require.NoError(t, err)

	select {
	case delta := <-frl.recordCh:
		assert.Positive(t, delta, "reconcile delta should be actualTotal - estimate")
		assert.Greater(t, delta, 7000, "9000 actual minus ~1000 estimate ≈ 8000")
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile RecordTokens was not called within timeout")
	}
}

// TestRouter_ReconcileTokens_SkippedWhenActualUnderEstimate — when the provider
// reports fewer tokens than the pre-flight estimate, the clamped delta is 0 and
// no reconcile fires (the estimate already charged the gate; over-estimates are
// never credited back).
func TestRouter_ReconcileTokens_SkippedWhenActualUnderEstimate(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	frl := &fakeRateLimiter{allowed: true, recordCh: make(chan int, 1)}

	stub := &stubProvider{
		name:     "openai",
		response: &llm.ChatResponse{Content: "ok", Usage: llm.TokenUsage{TotalTokens: 5}},
	}
	r := llm.NewRouter(registry,
		llm.WithProvider(stub),
		llm.WithRateLimitChecker(frl),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:    "gpt-4",
		UserID:   uuid.New(),
		Tier:     "free",
		Messages: []llm.Message{{Role: "user", Content: strings.Repeat("a", 4000)}}, // est ~1000 > 5
	})
	require.NoError(t, err)

	select {
	case delta := <-frl.recordCh:
		t.Fatalf("reconcile must not fire when actual <= estimate, got delta=%d", delta)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestRouter_DailySpendError_PropagatesAsIs — sentinel reaches caller.
func TestRouter_DailySpendError_PropagatesAsIs(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimitChecker(&fakeRateLimiter{err: llm.ErrDailySpendExceeded}),
	)
	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "gpt-4",
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
	})
	assert.True(t, errors.Is(err, llm.ErrDailySpendExceeded), "got %v", err)
}

// TestRouter_RateLimitUnavailable_PropagatesAsIs — infra sentinel pass-through.
func TestRouter_RateLimitUnavailable_PropagatesAsIs(t *testing.T) {
	entry := healthyEntry("gpt-4", "openai", 5.0, 15.0, 300)
	registry := newTestRegistry(entry)
	r := llm.NewRouter(registry,
		llm.WithProvider(makeStub("openai")),
		llm.WithRateLimitChecker(&fakeRateLimiter{err: llm.ErrRateLimitUnavailable}),
	)
	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "gpt-4",
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
	})
	assert.True(t, errors.Is(err, llm.ErrRateLimitUnavailable), "got %v", err)
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
	businessID := uuid.New()

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
		BusinessID: uuid.Nil,
	})
	require.NoError(t, err)

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

// ---------------------------------------------------------------------------
// Router retry-once tests (sibling registry entry on transient errors)
// ---------------------------------------------------------------------------

// sequenceProvider returns canned (response, error) tuples in order, one per
// Chat call. Tests assemble its responses up-front so the multi-attempt
// retry path can be driven deterministically.
type sequenceProvider struct {
	name      string
	responses []seqResponse
	callCount int
}

type seqResponse struct {
	resp *llm.ChatResponse
	err  error
}

func (p *sequenceProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if p.callCount >= len(p.responses) {
		return nil, fmt.Errorf("%s: unexpected extra call (count=%d)", p.name, p.callCount+1)
	}
	r := p.responses[p.callCount]
	p.callCount++
	return r.resp, r.err
}

func (p *sequenceProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}
func (p *sequenceProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p *sequenceProvider) HealthCheck(_ context.Context) error                   { return nil }
func (p *sequenceProvider) Name() string                                          { return p.name }

// candidateSelector hands back a fixed Candidates list plus captures every
// Record call so tests can assert health-rollup fan-out across both
// retry attempts.
type candidateSelector struct {
	candidates []llm.Candidate
	pickErr    error
	records    []recordedOutcome
}

type recordedOutcome struct {
	entry   *llm.ModelProviderEntry
	outcome llm.Outcome
}

func (s *candidateSelector) Pick(_ string, _ llm.Strategy) (*llm.ModelProviderEntry, llm.Provider, error) {
	if s.pickErr != nil {
		return nil, nil, s.pickErr
	}
	if len(s.candidates) == 0 {
		return nil, nil, llm.ErrNoProvider
	}
	return s.candidates[0].Entry, s.candidates[0].Provider, nil
}

func (s *candidateSelector) Candidates(_ string, _ llm.Strategy) []llm.Candidate {
	return s.candidates
}

func (s *candidateSelector) Record(entry *llm.ModelProviderEntry, o llm.Outcome) {
	s.records = append(s.records, recordedOutcome{entry, o})
}

func transientAPIError(status int) error {
	return fmt.Errorf("provider chat: %w", &openai.APIError{HTTPStatusCode: status, Message: "transient"})
}

func nonTransientAPIError(status int) error {
	return fmt.Errorf("provider chat: %w", &openai.APIError{HTTPStatusCode: status, Message: "client error"})
}

func TestRouter_RetryOnce_TransientThenSuccess(t *testing.T) {
	before := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("success", "second"))

	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	entryB := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "anthropic"}
	provA := &sequenceProvider{name: "openrouter", responses: []seqResponse{
		{nil, transientAPIError(503)},
	}}
	provB := &sequenceProvider{name: "anthropic", responses: []seqResponse{
		{&llm.ChatResponse{Content: "ok", Usage: llm.TokenUsage{InputTokens: 100, OutputTokens: 50}}, nil},
	}}
	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
		{Entry: entryB, Provider: provB},
	}}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	resp, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)
	assert.Equal(t, "anthropic", resp.Provider)
	assert.Equal(t, 1, provA.callCount)
	assert.Equal(t, 1, provB.callCount)

	after := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("success", "second"))
	assert.Equal(t, before+1, after, "success on the retry must bump success/second")
}

func TestRouter_RetryOnce_TransientThenTransient(t *testing.T) {
	before := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("exhausted", "second"))

	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	entryB := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "anthropic"}
	provA := &sequenceProvider{name: "openrouter", responses: []seqResponse{
		{nil, transientAPIError(503)},
	}}
	provB := &sequenceProvider{name: "anthropic", responses: []seqResponse{
		{nil, transientAPIError(503)},
	}}
	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
		{Entry: entryB, Provider: provB},
	}}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.Error(t, err)
	assert.Equal(t, 1, provA.callCount)
	assert.Equal(t, 1, provB.callCount,
		"sibling must be attempted even though it also fails")

	after := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("exhausted", "second"))
	assert.Equal(t, before+1, after)
}

func TestRouter_RetryOnce_NonTransientImmediate(t *testing.T) {
	before := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("nonretryable", "first"))

	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	entryB := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "anthropic"}
	provA := &sequenceProvider{name: "openrouter", responses: []seqResponse{
		{nil, nonTransientAPIError(400)},
	}}
	provB := &sequenceProvider{name: "anthropic"}
	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
		{Entry: entryB, Provider: provB},
	}}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.Error(t, err)
	assert.Equal(t, 1, provA.callCount)
	assert.Equal(t, 0, provB.callCount,
		"non-transient errors must not consume the retry budget")

	after := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("nonretryable", "first"))
	assert.Equal(t, before+1, after)
}

func TestRouter_RetryOnce_NonTransientOnRetry(t *testing.T) {
	before := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("nonretryable", "second"))

	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	entryB := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "anthropic"}
	provA := &sequenceProvider{name: "openrouter", responses: []seqResponse{
		{nil, transientAPIError(503)},
	}}
	provB := &sequenceProvider{name: "anthropic", responses: []seqResponse{
		{nil, nonTransientAPIError(400)},
	}}
	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
		{Entry: entryB, Provider: provB},
	}}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.Error(t, err)
	assert.Equal(t, 1, provA.callCount)
	assert.Equal(t, 1, provB.callCount)

	after := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("nonretryable", "second"))
	assert.Equal(t, before+1, after)
}

func TestRouter_RetryOnce_SingleCandidate(t *testing.T) {
	before := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("exhausted", "first"))

	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	provA := &sequenceProvider{name: "openrouter", responses: []seqResponse{
		{nil, transientAPIError(503)},
	}}
	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
	}}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.Error(t, err)
	assert.Equal(t, 1, provA.callCount)

	after := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("exhausted", "first"))
	assert.Equal(t, before+1, after,
		"single-candidate transient must bump exhausted/first, not exhausted/second")
}

func TestRouter_RetryOnce_FirstAttemptSucceeds(t *testing.T) {
	before := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("success", "first"))

	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	entryB := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "anthropic"}
	provA := &sequenceProvider{name: "openrouter", responses: []seqResponse{
		{&llm.ChatResponse{Content: "first ok"}, nil},
	}}
	provB := &sequenceProvider{name: "anthropic"}
	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
		{Entry: entryB, Provider: provB},
	}}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	resp, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.NoError(t, err)
	assert.Equal(t, "first ok", resp.Content)
	assert.Equal(t, "openrouter", resp.Provider)
	assert.Equal(t, 1, provA.callCount)
	assert.Equal(t, 0, provB.callCount)

	after := testutil.ToFloat64(metrics.LLMRouterRetry.WithLabelValues("success", "first"))
	assert.Equal(t, before+1, after)
}

// Bills-only-success: when the retry succeeds, the billing fake must
// capture exactly one UsageLog and that log's Usage matches B's response.
// A's partial Usage (mimicking SDKs that surface a shell on error) must
// NOT bleed into the billing row.
func TestRouter_RetryOnce_BillsOnlySuccess(t *testing.T) {
	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter", InputCostPer1MTok: 1, OutputCostPer1MTok: 3}
	entryB := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "anthropic", InputCostPer1MTok: 2, OutputCostPer1MTok: 6}
	provA := &sequenceProvider{name: "openrouter", responses: []seqResponse{
		{&llm.ChatResponse{Usage: llm.TokenUsage{InputTokens: 99, OutputTokens: 99}}, transientAPIError(503)},
	}}
	provB := &sequenceProvider{name: "anthropic", responses: []seqResponse{
		{&llm.ChatResponse{Content: "winner", Usage: llm.TokenUsage{InputTokens: 100, OutputTokens: 50}}, nil},
	}}
	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
		{Entry: entryB, Provider: provB},
	}}

	billing := &MockBillingRepository{}
	r := llm.NewRouter(llm.NewRegistry(),
		llm.WithSelector(sel),
		llm.WithBilling(billing),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "gpt-4",
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
	})
	require.NoError(t, err)
	assert.Equal(t, "winner", resp.Content)

	assert.Eventually(t, func() bool { return billing.LastLog() != nil },
		200*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, 1, billing.CallCount(),
		"LogUsage must fire exactly once — the failed attempt is never billed")

	last := billing.LastLog()
	require.NotNil(t, last)
	assert.Equal(t, 100, last.InputTokens,
		"billed Usage must come from the successful sibling, not the failed primary")
	assert.Equal(t, 50, last.OutputTokens)
	assert.Equal(t, "anthropic", last.Provider)
}

func TestRouter_RetryOnce_RecordsBothOutcomes(t *testing.T) {
	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	entryB := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "anthropic"}
	provA := &sequenceProvider{name: "openrouter", responses: []seqResponse{
		{nil, transientAPIError(503)},
	}}
	provB := &sequenceProvider{name: "anthropic", responses: []seqResponse{
		{&llm.ChatResponse{Content: "ok"}, nil},
	}}
	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
		{Entry: entryB, Provider: provB},
	}}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.NoError(t, err)

	require.Len(t, sel.records, 2,
		"health rollup must see one Record per attempt")
	assert.False(t, sel.records[0].outcome.Success,
		"attempt 0 failed and Record must reflect that")
	assert.True(t, sel.records[1].outcome.Success,
		"attempt 1 succeeded and Record must reflect that")
	assert.Equal(t, "openrouter", sel.records[0].entry.Provider)
	assert.Equal(t, "anthropic", sel.records[1].entry.Provider)
}

func TestRouter_RetryOnce_NoCandidates(t *testing.T) {
	sel := &candidateSelector{candidates: nil}
	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	_, err := r.Chat(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	assert.ErrorIs(t, err, llm.ErrNoProvider)
	assert.Empty(t, sel.records, "no provider was tried, no Record fires")
}

func TestRouter_RetryOnce_RateLimitErrorNotRetried(t *testing.T) {
	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	provA := &sequenceProvider{name: "openrouter"}

	sel := &candidateSelector{candidates: []llm.Candidate{{Entry: entryA, Provider: provA}}}
	r := llm.NewRouter(llm.NewRegistry(),
		llm.WithSelector(sel),
		llm.WithRateLimitChecker(&fakeRateLimiter{err: llm.ErrDailySpendExceeded}),
	)

	_, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      "gpt-4",
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
	})
	assert.ErrorIs(t, err, llm.ErrDailySpendExceeded)
	assert.Equal(t, 0, provA.callCount,
		"rate-limit gate must fire before any candidate is dialed")
}

// ChatStream is intentionally not retried — verify it walks Pick (single
// candidate path) and does not consume the retry budget. We assert by
// counting Records: only one fires, regardless of how many candidates
// the selector advertises.
func TestRouter_RetryOnce_ChatStreamUnchanged(t *testing.T) {
	entryA := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "openrouter"}
	entryB := &llm.ModelProviderEntry{Model: "gpt-4", Provider: "anthropic"}
	provA := makeStub("openrouter")
	provB := makeStub("anthropic")

	sel := &candidateSelector{candidates: []llm.Candidate{
		{Entry: entryA, Provider: provA},
		{Entry: entryB, Provider: provB},
	}}

	r := llm.NewRouter(llm.NewRegistry(), llm.WithSelector(sel))
	ch, err := r.ChatStream(context.Background(), llm.ChatRequest{Model: "gpt-4"})
	require.NoError(t, err)
	require.NotNil(t, ch)

	assert.Len(t, sel.records, 1,
		"ChatStream must walk Pick (single candidate), not the retry list")
}

// End-to-end shape: register two real registry entries for the same model
// (one openai, one anthropic). Drive Chat through Router. Verify the
// successful response carries the sibling's Provider name and the
// captured billing row attributes to the sibling.
func TestRouter_RetryOnce_E2E_OpenAIToAnthropic(t *testing.T) {
	const model = "anthropic/claude-sonnet-4-6"
	openaiEntry := healthyEntry(model, "openai", 3.0, 15.0, 200)
	anthropicEntry := healthyEntry(model, "anthropic", 3.0, 15.0, 250)
	registry := newTestRegistry(openaiEntry, anthropicEntry)

	openaiFailing := &sequenceProvider{
		name: "openai",
		responses: []seqResponse{
			{nil, transientAPIError(503)},
		},
	}
	anthropicWinning := &sequenceProvider{
		name: "anthropic",
		responses: []seqResponse{
			{&llm.ChatResponse{
				Content: "hello from anthropic",
				Usage:   llm.TokenUsage{InputTokens: 250, OutputTokens: 80},
			}, nil},
		},
	}

	billing := &MockBillingRepository{}
	r := llm.NewRouter(registry,
		llm.WithProvider(openaiFailing),
		llm.WithProvider(anthropicWinning),
		llm.WithBilling(billing),
	)

	resp, err := r.Chat(context.Background(), llm.ChatRequest{
		Model:      model,
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
	})
	require.NoError(t, err)
	assert.Equal(t, "hello from anthropic", resp.Content)
	assert.Equal(t, "anthropic", resp.Provider,
		"successful response must attribute to the sibling that served it")
	assert.Equal(t, 1, openaiFailing.callCount)
	assert.Equal(t, 1, anthropicWinning.callCount)

	assert.Eventually(t, func() bool { return billing.LastLog() != nil },
		200*time.Millisecond, 10*time.Millisecond)
	last := billing.LastLog()
	require.NotNil(t, last)
	assert.Equal(t, "anthropic", last.Provider)
	assert.Equal(t, 250, last.InputTokens)
	assert.Equal(t, 80, last.OutputTokens)
	assert.Equal(t, 1, billing.CallCount(),
		"the failed openai attempt must NOT have been billed")
}
