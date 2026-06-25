package wire

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/api/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const (
	billingEventuallyTimeout = 500 * time.Millisecond
	billingEventuallyTick    = 10 * time.Millisecond
)

// countingBillingRepo is a minimal llm.BillingRepository that increments an
// atomic counter on each LogUsage call so the titler-Router billing test can
// prove buildTitlerRouter wired WithBilling and logBilling fired. The read
// methods are unused stubs — only the write path is exercised.
type countingBillingRepo struct {
	calls int64
}

func (c *countingBillingRepo) LogUsage(_ context.Context, _ *llm.UsageLog) error {
	atomic.AddInt64(&c.calls, 1)
	return nil
}

func (c *countingBillingRepo) GetUserBalance(context.Context, uuid.UUID) (float64, error) {
	return 0, nil
}

func (c *countingBillingRepo) GetDailySpend(context.Context, uuid.UUID, time.Time) (float64, error) {
	return 0, nil
}

func (c *countingBillingRepo) GetMonthlyUsage(context.Context, uuid.UUID, int, int) ([]llm.UsageLog, error) {
	return nil, nil
}

// titlerFakeProvider returns a canned ChatResponse with non-zero token usage
// so the Router computes a billable cost and forwards a UsageLog.
type titlerFakeProvider struct {
	name string
	resp *llm.ChatResponse
}

func (p *titlerFakeProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return p.resp, nil
}

func (p *titlerFakeProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, nil
}
func (p *titlerFakeProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (p *titlerFakeProvider) HealthCheck(_ context.Context) error                   { return nil }
func (p *titlerFakeProvider) Name() string                                          { return p.name }

// titlerFakeSelector pins a single (entry, provider) candidate so the Router's
// Chat path resolves without touching the real provider HTTP stack.
type titlerFakeSelector struct {
	entry *llm.ModelProviderEntry
	prov  llm.Provider
}

func (s *titlerFakeSelector) Pick(_ string, _ llm.Strategy) (*llm.ModelProviderEntry, llm.Provider, error) {
	return s.entry, s.prov, nil
}

func (s *titlerFakeSelector) Candidates(_ string, _ llm.Strategy) []llm.Candidate {
	return []llm.Candidate{{Entry: s.entry, Provider: s.prov}}
}

func (s *titlerFakeSelector) Record(*llm.ModelProviderEntry, llm.Outcome) {}

// titlerTestConfig is the cheap-tier titler config (haiku titler over sonnet
// main) with a single provider key so LLMProviderOpts returns one option.
func titlerTestConfig() *config.Config {
	return &config.Config{
		LLMModel:        "anthropic/claude-sonnet-4-6",
		TitlerModel:     "anthropic/claude-haiku-4-5",
		AnthropicAPIKey: "sk-ant-test",
		RedisDownPolicy: "block",
	}
}

// titlerFakeSelectorOpt injects a fake selector so the production
// buildTitlerRouter resolves without a real provider HTTP call.
func titlerFakeSelectorOpt() llm.RouterOption {
	return llm.WithSelector(&titlerFakeSelector{
		entry: &llm.ModelProviderEntry{
			Model: "anthropic/claude-haiku-4-5", Provider: "anthropic",
			InputCostPer1MTok: 1.00, OutputCostPer1MTok: 5.00,
			HealthStatus: llm.HealthStatusHealthy, Enabled: true,
		},
		prov: &titlerFakeProvider{name: "anthropic", resp: &llm.ChatResponse{
			Content: "Запуск нового кафе", FinishReason: "stop",
			Usage: llm.TokenUsage{InputTokens: 200, OutputTokens: 12},
		}},
	})
}

// titlerChatRequest is the BusinessID-bearing, background-tier completion the
// auto-titler issues on every chat turn (see service/titler.go GenerateAndSave).
func titlerChatRequest() llm.ChatRequest {
	return llm.ChatRequest{
		UserID:     uuid.Nil,
		BusinessID: uuid.New(),
		Model:      "anthropic/claude-haiku-4-5",
		Messages:   []llm.Message{{Role: "user", Content: "hi"}},
		Tier:       "background",
	}
}

// TestBuildTitlerRouter_WritesUsageLog_WhenBillingWired is the fail-on-revert
// guard for the titler-billing fix. The api-side titler Router (built by the
// production buildTitlerRouter that BuildServices calls) must be wired WITH
// billing so a titler-style completion (real BusinessID) writes a usage_logs
// row — otherwise the per-business daily-spend cap (GetDailySpend sums
// usage_logs) under-counts by the entire titler volume. Deleting the
// WithBilling wiring in buildTitlerRouter leaves r.billing nil and this
// assertion fails.
func TestBuildTitlerRouter_WritesUsageLog_WhenBillingWired(t *testing.T) {
	billing := &countingBillingRepo{}

	router, err := buildTitlerRouter(titlerTestConfig(), discardLogger(), nil, billing, titlerFakeSelectorOpt())
	require.NoError(t, err)
	require.NotNil(t, router, "titler router must construct when TITLER_MODEL and a provider key are set")

	_, err = router.Chat(context.Background(), titlerChatRequest())
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		return atomic.LoadInt64(&billing.calls) >= 1
	}, billingEventuallyTimeout, billingEventuallyTick,
		"titler Router must write a usage_logs row via WithBilling — the daily-spend cap depends on it")
}

// TestBuildTitlerRouter_NoUsageLog_WhenBillingNil proves the guard is
// load-bearing: with a nil billing repo (the pre-fix state, where the rate
// limiter was also disabled) buildTitlerRouter wires no billing sink, so
// logBilling never fires and no usage_logs row is written.
func TestBuildTitlerRouter_NoUsageLog_WhenBillingNil(t *testing.T) {
	billing := &countingBillingRepo{}

	router, err := buildTitlerRouter(titlerTestConfig(), discardLogger(), nil, nil, titlerFakeSelectorOpt())
	require.NoError(t, err)
	require.NotNil(t, router)

	_, err = router.Chat(context.Background(), titlerChatRequest())
	require.NoError(t, err)

	assert.Never(t, func() bool {
		return atomic.LoadInt64(&billing.calls) >= 1
	}, billingEventuallyTimeout, billingEventuallyTick,
		"with billing unwired the titler Router must not write usage_logs")
}

// TestLLMProviderOpts_RegistersTitlerModel — set LLM_MODEL and TITLER_MODEL to
// two different IDs (the .env.example default) and assert the registry holds
// an entry for TITLER_MODEL on the anthropic provider. Before the fix the
// titler Router could not Pick a provider for TITLER_MODEL and every
// auto-title attempt failed silently.
func TestLLMProviderOpts_RegistersTitlerModel(t *testing.T) {
	cfg := &config.Config{
		LLMModel:        "anthropic/claude-sonnet-4-6",
		TitlerModel:     "anthropic/claude-haiku-4-5",
		AnthropicAPIKey: "sk-ant-test",
	}
	reg := llm.NewRegistry()

	opts := LLMProviderOpts(cfg, reg, discardLogger())
	require.NotEmpty(t, opts, "at least one provider option must be returned when AnthropicAPIKey is set")

	mainEntries := reg.GetModelProviders("anthropic/claude-sonnet-4-6")
	require.Len(t, mainEntries, 1, "LLMModel must be registered")
	assert.Equal(t, "anthropic", mainEntries[0].Provider)

	titlerEntries := reg.GetModelProviders("anthropic/claude-haiku-4-5")
	require.Len(t, titlerEntries, 1, "TitlerModel must be registered — this is the WR-02 regression guard")
	assert.Equal(t, "anthropic", titlerEntries[0].Provider)

	assert.InDelta(t, 3.00, mainEntries[0].InputCostPer1MTok, 1e-9, "sonnet input price")
	assert.InDelta(t, 15.00, mainEntries[0].OutputCostPer1MTok, 1e-9, "sonnet output price")
	assert.InDelta(t, 1.00, titlerEntries[0].InputCostPer1MTok, 1e-9, "haiku input price")
	assert.InDelta(t, 5.00, titlerEntries[0].OutputCostPer1MTok, 1e-9, "haiku output price")
}

// TestLLMProviderOpts_DedupesWhenTitlerEqualsMain — TitlerModel == LLMModel
// (default graceful fallback in config.Load) must not double-register the
// same (provider, model) pair.
func TestLLMProviderOpts_DedupesWhenTitlerEqualsMain(t *testing.T) {
	cfg := &config.Config{
		LLMModel:        "anthropic/claude-sonnet-4-6",
		TitlerModel:     "anthropic/claude-sonnet-4-6",
		AnthropicAPIKey: "sk-ant-test",
	}
	reg := llm.NewRegistry()

	_ = LLMProviderOpts(cfg, reg, discardLogger())

	entries := reg.GetModelProviders("anthropic/claude-sonnet-4-6")
	assert.Len(t, entries, 1, "TitlerModel == LLMModel must dedupe to a single (provider, model) entry")
}

// TestLLMProviderOpts_RegistersEveryProviderForBothModels — when multiple
// API keys are set, each (provider, model) pair must surface in the registry
// so the Router can fail over from one provider to the next on the same model.
func TestLLMProviderOpts_RegistersEveryProviderForBothModels(t *testing.T) {
	cfg := &config.Config{
		LLMModel:         "anthropic/claude-sonnet-4-6",
		TitlerModel:      "anthropic/claude-haiku-4-5",
		AnthropicAPIKey:  "sk-ant-test",
		OpenRouterAPIKey: "sk-or-test",
	}
	reg := llm.NewRegistry()

	_ = LLMProviderOpts(cfg, reg, discardLogger())

	for _, model := range []string{"anthropic/claude-sonnet-4-6", "anthropic/claude-haiku-4-5"} {
		entries := reg.GetModelProviders(model)
		providers := make(map[string]bool, len(entries))
		for _, e := range entries {
			providers[e.Provider] = true
		}
		assert.True(t, providers["anthropic"], "anthropic provider must register %s", model)
		assert.True(t, providers["openrouter"], "openrouter provider must register %s", model)
	}
}

// TestPriceFor_KnownModel — pin sonnet/haiku/opus/gpt-4o-mini prices. A
// rate-card edit must update docs/llm-pricing.md AND this test in lockstep
// with the orchestrator-side copy.
func TestPriceFor_KnownModel(t *testing.T) {
	cases := []struct {
		model   string
		wantIn  float64
		wantOut float64
	}{
		{"anthropic/claude-sonnet-4-6", 3.00, 15.00},
		{"anthropic/claude-haiku-4-5", 1.00, 5.00},
		{"anthropic/claude-opus-4-7", 5.00, 25.00},
		{"openai/gpt-4o-mini", 0.15, 0.60},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			in, out := priceFor(tc.model)
			assert.InDelta(t, tc.wantIn, in, 1e-9)
			assert.InDelta(t, tc.wantOut, out, 1e-9)
		})
	}
}

// TestPriceFor_UnknownModel_ZeroZero — unknown model returns (0, 0).
func TestPriceFor_UnknownModel_ZeroZero(t *testing.T) {
	in, out := priceFor("nonexistent/model")
	assert.Equal(t, 0.0, in)
	assert.Equal(t, 0.0, out)
}
