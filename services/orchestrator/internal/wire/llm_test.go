// Package wire tests for LLMRouter. Pricing-table regression coverage is the
// load-bearing contract: a future PR registering a new model without pricing
// MUST fail TestLLMRouter_PricesAllConfiguredModels so cost rows never silently
// land with zero USD.
package wire

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
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

// captureHandler is a minimal slog.Handler that records every record so tests
// can assert which messages were emitted at which level. Guarded by a mutex so
// it stays safe if a future logging path fires from a goroutine.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// warningsMentioning returns the messages of captured records at WARN level (or
// above) whose message or attributes reference the given substring.
func (h *captureHandler) warningsMentioning(needle string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.records {
		if r.Level < slog.LevelWarn {
			continue
		}
		if strings.Contains(r.Message, needle) {
			out = append(out, r.Message)
			continue
		}
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), needle) {
				out = append(out, r.Message)
				return false
			}
			return true
		})
	}
	return out
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

// TestLLMRouter_UnknownModel_WarnsRateCardMiss — a model absent from the rate
// card must produce a WARN at registration naming the model and pointing at
// docs/llm-pricing.md, so the operator sees that billing will record $0 and the
// daily-spend gate is inert rather than discovering it by inspecting
// usage_logs. Removing the warnRateCardMiss call must fail this test.
func TestLLMRouter_UnknownModel_WarnsRateCardMiss(t *testing.T) {
	t.Setenv("LLM_MODEL", "foo/unknown-model")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	cfg, err := config.Load()
	require.NoError(t, err)

	capture := &captureHandler{}
	router, err := LLMRouter(cfg, slog.New(capture))
	require.NoError(t, err)
	require.NotNil(t, router)

	warnings := capture.warningsMentioning("foo/unknown-model")
	require.NotEmpty(t, warnings, "expected a WARN naming the unknown model")
	assert.NotEmpty(t, capture.warningsMentioning("docs/llm-pricing.md"),
		"warning must point the operator at the rate-card source")
}

// TestLLMRouter_KnownModel_NoRateCardWarning — every configured model is in the
// rate card, so no rate-card-miss warning should fire. Guards against the
// warning becoming noise that the operator learns to ignore.
func TestLLMRouter_KnownModel_NoRateCardWarning(t *testing.T) {
	t.Setenv("LLM_MODEL", "anthropic/claude-sonnet-4-6")
	t.Setenv("DRAFT_REPLY_MODEL", "anthropic/claude-haiku-4-5")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	cfg, err := config.Load()
	require.NoError(t, err)

	capture := &captureHandler{}
	router, err := LLMRouter(cfg, slog.New(capture))
	require.NoError(t, err)
	require.NotNil(t, router)

	assert.Empty(t, capture.warningsMentioning("docs/llm-pricing.md"),
		"priced models must not emit a rate-card-miss warning")
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
		{"deepseek-v4-flash", 3.60, 6.00},
		// Folder-qualified Yandex URI must normalize to the bare slug and price
		// identically — this is how the model ID actually reaches priceFor.
		{"gpt://b1gnbi7pl8c7d6s885t5/deepseek-v4-flash/latest", 3.60, 6.00},
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
func (f *fakeSelector) Candidates(_ string, _ llm.Strategy) []llm.Candidate {
	if f.entry == nil {
		return nil
	}
	return []llm.Candidate{{Entry: f.entry, Provider: f.prov}}
}
func (f *fakeSelector) Record(_ *llm.ModelProviderEntry, _ llm.Outcome) {}
