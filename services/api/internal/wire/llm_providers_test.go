package wire

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/api/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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

	// Both LLMModel and TitlerModel must be reachable through the registry.
	mainEntries := reg.GetModelProviders("anthropic/claude-sonnet-4-6")
	require.Len(t, mainEntries, 1, "LLMModel must be registered")
	assert.Equal(t, "anthropic", mainEntries[0].Provider)

	titlerEntries := reg.GetModelProviders("anthropic/claude-haiku-4-5")
	require.Len(t, titlerEntries, 1, "TitlerModel must be registered — this is the WR-02 regression guard")
	assert.Equal(t, "anthropic", titlerEntries[0].Provider)

	// Both entries must carry non-zero pricing so usage_logs rows surface
	// actual cost. WR-02 also called out that registered entries previously
	// landed at $0 even when they existed.
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
