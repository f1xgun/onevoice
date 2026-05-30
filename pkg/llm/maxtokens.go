package llm

import "strings"

// Provider-agnostic per-model MaxTokens defaults.
//
// The orchestrator's agent-loop builds a ChatRequest without knowing which
// provider will serve it (the router picks based on registered API keys).
// The Anthropic-side defaults in pkg/llm/providers only fire on the direct
// Anthropic path — when traffic routes through OpenRouter the request reaches
// the OpenRouter API with no max_tokens cap, and OpenRouter then asks the
// model for its full output window (e.g. 65_536 tokens for Sonnet 4.6) which
// trips paid-credit limits.
//
// This map lives at the pkg/llm boundary so step.go (and any other caller)
// can stamp a sensible cap regardless of which provider will execute the
// request. Keys cover the OpenRouter `provider/model` namespace AND the raw
// Anthropic-API ids; lookup is tried in both forms (with and without the
// `provider/` prefix) to keep the call sites trivial.
//
// Values are the same conservative ceilings the Anthropic provider uses
// internally (Sonnet → 8192, Haiku → 4096) so the cap is consistent across
// providers. Unknown models fall back to maxTokensUnknownDefault.
const (
	maxTokensSonnetDefault  = 8192
	maxTokensHaikuDefault   = 4096
	maxTokensOpusDefault    = 8192
	maxTokensUnknownDefault = 4096
)

var maxTokensByModel = map[string]int{
	// Anthropic — raw ids
	"claude-sonnet-4-6":          maxTokensSonnetDefault,
	"claude-haiku-4-5":           maxTokensHaikuDefault,
	"claude-haiku-4-5-20251001":  maxTokensHaikuDefault,
	"claude-opus-4-7":            maxTokensOpusDefault,
	"claude-opus-4-6":            maxTokensOpusDefault,
	"claude-sonnet-4-5":          maxTokensSonnetDefault,
	"claude-sonnet-4-5-20250929": maxTokensSonnetDefault,
}

// DefaultMaxTokensFor returns a conservative MaxTokens cap for the given
// model id. Accepts both raw ids ("claude-sonnet-4-6") and OpenRouter-style
// prefixed ids ("anthropic/claude-sonnet-4-6"). Unknown models return
// maxTokensUnknownDefault (4096) — small enough to fit free-tier OpenRouter
// budgets, large enough to produce useful agent-loop responses.
func DefaultMaxTokensFor(model string) int {
	if v, ok := maxTokensByModel[model]; ok {
		return v
	}
	if i := strings.IndexByte(model, '/'); i >= 0 {
		if v, ok := maxTokensByModel[model[i+1:]]; ok {
			return v
		}
	}
	return maxTokensUnknownDefault
}
