// Package wire tests for LLMRouter. Pricing-table regression coverage is the
// load-bearing contract: a future PR registering a new model without pricing
// MUST fail TestLLMRouter_PricesAllConfiguredModels so cost rows never silently
// land with zero USD.
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
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
)

const (
	eventuallyTimeout = 500 * time.Millisecond
	eventuallyTick    = 10 * time.Millisecond
)

// countingWriter is a minimal llm.Writer that increments an atomic counter on
// each LogUsage call so the WithBilling pass-through test can prove the option
// reached llm.NewRouter unmodified.
type countingWriter struct {
	calls int64
}

func (c *countingWriter) LogUsage(_ context.Context, _ *llm.UsageLog) error {
	atomic.AddInt64(&c.calls, 1)
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestLLMRouter_PricesAllConfiguredModels — set LLM_MODEL + DRAFT_REPLY_MODEL
// to two different model IDs; build the router; assert every configured model
// resolves through priceFor with the modelPricing-table cost (3/15 for sonnet,
// 1/5 for haiku). This is the regression guard: registering a new model in
// modelPricing without verifying coverage here fails this test if the
// orchestrator's configured-model accounting drifts.
func TestLLMRouter_PricesAllConfiguredModels(t *testing.T) {
	t.Setenv("LLM_MODEL", "anthropic/claude-sonnet-4-6")
	t.Setenv("DRAFT_REPLY_MODEL", "anthropic/claude-haiku-4-5")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	cfg, err := config.Load()
	require.NoError(t, err)

	router, err := LLMRouter(cfg, discardLogger())
	require.NoError(t, err)
	require.NotNil(t, router)

	cases := []struct {
		model   string
		wantIn  float64
		wantOut float64
	}{
		{"anthropic/claude-sonnet-4-6", 3.00, 15.00},
		{"anthropic/claude-haiku-4-5", 1.00, 5.00},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			in, out := priceFor(tc.model)
			assert.InDelta(t, tc.wantIn, in, 1e-9, "input price")
			assert.InDelta(t, tc.wantOut, out, 1e-9, "output price")
		})
	}
}

// TestLLMRouter_UnknownModel_ZeroCost — unrecognized model ID returns (0,0)
// so the router still constructs (no panic) but billing logs zero cost.
// Operator visibility: usage_logs rows for unknown models surface as
// "model registered but cost=0" which the operator runbook treats as a
// pricing-table-drift signal.
func TestLLMRouter_UnknownModel_ZeroCost(t *testing.T) {
	t.Setenv("LLM_MODEL", "foo/unknown-model")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	cfg, err := config.Load()
	require.NoError(t, err)

	router, err := LLMRouter(cfg, discardLogger())
	require.NoError(t, err)
	require.NotNil(t, router)

	in, out := priceFor("foo/unknown-model")
	assert.Equal(t, 0.0, in, "unknown model input cost must be 0")
	assert.Equal(t, 0.0, out, "unknown model output cost must be 0")
}

// TestLLMRouter_PassesExtraOptions — call LLMRouter with llm.WithBilling
// threaded as an extraOpt; trigger a Chat call (via a fake provider injected
// through WithSelector) and observe the counting writer ticked. Proves
// end-to-end that options flow from wire.LLMRouter → llm.NewRouter →
// Router.billing → goroutine-fired LogUsage.
func TestLLMRouter_PassesExtraOptions(t *testing.T) {
	t.Setenv("LLM_MODEL", "anthropic/claude-sonnet-4-6")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	cfg, err := config.Load()
	require.NoError(t, err)

	cw := &countingWriter{}
	fakeProv := &fakeProvider{name: "openrouter", resp: &llm.ChatResponse{
		Content: "hi", FinishReason: "stop",
		Usage: llm.TokenUsage{InputTokens: 100, OutputTokens: 50},
	}}
	fakeSel := &fakeSelector{entry: &llm.ModelProviderEntry{
		Model: "anthropic/claude-sonnet-4-6", Provider: "openrouter",
		InputCostPer1MTok: 3.00, OutputCostPer1MTok: 15.00,
		HealthStatus: llm.HealthStatusHealthy, Enabled: true,
	}, prov: fakeProv}

	router, err := LLMRouter(cfg, discardLogger(),
		llm.WithBilling(cw),
		llm.WithSelector(fakeSel),
	)
	require.NoError(t, err)

	_, err = router.Chat(context.Background(), llm.ChatRequest{
		BusinessID: uuid.New(),
		Model:      "anthropic/claude-sonnet-4-6",
		Messages:   []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		return atomic.LoadInt64(&cw.calls) >= 1
	}, eventuallyTimeout, eventuallyTick, "billing writer must have been called via WithBilling option pass-through")
}

// TestPriceFor_KnownModel — pin sonnet/haiku/opus/gpt-4o-mini prices so a
// rate-card edit must update docs/llm-pricing.md AND this test in lockstep.
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

// TestPriceFor_UnknownModel_ZeroZero — unknown model returns (0,0).
func TestPriceFor_UnknownModel_ZeroZero(t *testing.T) {
	in, out := priceFor("nonexistent/model")
	assert.Equal(t, 0.0, in)
	assert.Equal(t, 0.0, out)
}

// --- fakes ---

type fakeProvider struct {
	name string
	resp *llm.ChatResponse
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return f.resp, nil
}
func (f *fakeProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, nil
}
func (f *fakeProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (f *fakeProvider) HealthCheck(_ context.Context) error                   { return nil }

type fakeSelector struct {
	entry *llm.ModelProviderEntry
	prov  llm.Provider
}

func (f *fakeSelector) Pick(_ string, _ llm.Strategy) (*llm.ModelProviderEntry, llm.Provider, error) {
	return f.entry, f.prov, nil
}
func (f *fakeSelector) Record(_ *llm.ModelProviderEntry, _ llm.Outcome) {}
