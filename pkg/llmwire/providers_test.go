package llmwire

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// captureHandler records WARN+ records so tests can assert the rate-card-miss
// warning fires (and names the model + the rate-card doc).
type captureHandler struct {
	mu   sync.Mutex
	warn []string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level < slog.LevelWarn {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	parts := []string{r.Message}
	r.Attrs(func(a slog.Attr) bool {
		parts = append(parts, a.Value.String())
		return true
	})
	h.warn = append(h.warn, strings.Join(parts, " "))
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) mentioning(needle string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, w := range h.warn {
		if strings.Contains(w, needle) {
			out = append(out, w)
		}
	}
	return out
}

func TestRegisterConfiguredProviders_PricesKnownModels(t *testing.T) {
	reg := llm.NewRegistry()
	cap := &captureHandler{}

	opts := RegisterConfiguredProviders(reg, slog.New(cap),
		ProviderKeys{OpenRouter: "sk-or", Anthropic: "sk-ant"},
		[]string{"anthropic/claude-sonnet-4-6"},
		nil,
	)

	// One WithProvider option per keyed provider set (openrouter, anthropic).
	assert.Len(t, opts, 2)

	// Both providers registered the model with its rate-card price.
	entries := reg.GetModelProviders("anthropic/claude-sonnet-4-6")
	require.Len(t, entries, 2)
	for _, e := range entries {
		assert.InDelta(t, 3.00, e.InputCostPer1MTok, 1e-9)
		assert.InDelta(t, 15.00, e.OutputCostPer1MTok, 1e-9)
	}
	assert.Empty(t, cap.mentioning("docs/llm-pricing.md"), "priced model must not warn")
}

func TestRegisterConfiguredProviders_WarnsOnRateCardMiss(t *testing.T) {
	reg := llm.NewRegistry()
	cap := &captureHandler{}

	RegisterConfiguredProviders(reg, slog.New(cap),
		ProviderKeys{OpenRouter: "sk-or"},
		[]string{"foo/unknown-model"},
		nil,
	)

	require.NotEmpty(t, cap.mentioning("foo/unknown-model"), "unknown model must warn")
	assert.NotEmpty(t, cap.mentioning("docs/llm-pricing.md"), "warning must point at the rate-card doc")
}

func TestRegisterConfiguredProviders_SelfHostedEndpoint(t *testing.T) {
	reg := llm.NewRegistry()
	model := "gpt://folder/deepseek-v4-flash/latest"

	opts := RegisterConfiguredProviders(reg, slog.New(&captureHandler{}),
		ProviderKeys{},
		nil,
		[]llm.SelfHostedEndpoint{{URL: "https://vm/v1", Model: model, APIKey: "k"}},
	)

	assert.Len(t, opts, 1, "one WithProvider option for the self-hosted endpoint")
	entries := reg.GetModelProviders(model)
	require.Len(t, entries, 1)
	assert.Equal(t, "selfhosted-0", entries[0].Provider)
	// Folder-qualified id normalizes to deepseek-v4-flash → 3.60 / 6.00.
	assert.InDelta(t, 3.60, entries[0].InputCostPer1MTok, 1e-9)
	assert.InDelta(t, 6.00, entries[0].OutputCostPer1MTok, 1e-9)
}

func TestRegisterConfiguredProviders_NoKeys_NoOpts(t *testing.T) {
	reg := llm.NewRegistry()
	opts := RegisterConfiguredProviders(reg, slog.New(&captureHandler{}),
		ProviderKeys{}, []string{"anthropic/claude-sonnet-4-6"}, nil)
	assert.Empty(t, opts)
}
