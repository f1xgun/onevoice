package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPriceFor_KnownModel pins the rate-card prices so an accidental edit to the
// shared modelPricing map is caught. A rate-card edit must update this test AND
// docs/llm-pricing.md in lockstep. This is the single home for these numbers —
// the api and orchestrator wire packages both consult PriceFor, so there is no
// longer a per-service copy to drift.
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
		{"dall-e-3", 0.04, 0.04},
		{"deepseek-v4-flash", 3.60, 6.00},
		// Folder-qualified Yandex URI must normalize to the bare slug and price
		// identically — this is how the model ID actually reaches PriceFor.
		{"gpt://b1gnbi7pl8c7d6s885t5/deepseek-v4-flash/latest", 3.60, 6.00},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			in, out := PriceFor(tc.model)
			assert.InDelta(t, tc.wantIn, in, 1e-9)
			assert.InDelta(t, tc.wantOut, out, 1e-9)
		})
	}
}

// TestPriceFor_UnknownModel_ZeroZero — an unknown model returns (0, 0) so the
// router still constructs; the operator sees zero-cost usage_logs rows as a
// drift signal.
func TestPriceFor_UnknownModel_ZeroZero(t *testing.T) {
	in, out := PriceFor("nonexistent/model")
	assert.Equal(t, 0.0, in)
	assert.Equal(t, 0.0, out)
}

// TestNormalizeModelID covers the Yandex gpt:// URI reduction and pass-through.
func TestNormalizeModelID(t *testing.T) {
	cases := map[string]string{
		"gpt://folder-id/deepseek-v4-flash/latest": "deepseek-v4-flash",
		"gpt://folder-id/deepseek-v4-flash":        "deepseek-v4-flash",
		"anthropic/claude-sonnet-4-6":              "anthropic/claude-sonnet-4-6",
		"deepseek-v4-flash":                        "deepseek-v4-flash",
		"gpt://only-one-segment":                   "gpt://only-one-segment",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, NormalizeModelID(in))
		})
	}
}
