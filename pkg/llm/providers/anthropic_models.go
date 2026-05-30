package providers

// Per-model context-window sizes (in tokens) for the current Claude generation.
const (
	claudeSonnet4_6ContextLength = 1_000_000
	claudeHaiku4_5ContextLength  = 200_000
	claudeOpus4_7ContextLength   = 1_000_000
)

// defaultMaxTokensByModel resolves `MaxTokens` defaults that we send to the
// Anthropic API when the caller leaves `ChatRequest.MaxTokens=0`. Anthropic
// returns HTTP 400 if `max_tokens` is missing, so this fallback is mandatory
// for every Anthropic call (see Phase 24 RESEARCH §Pitfall #2).
//
// PRICING NOTE: as of 2026-05-30 per platform.claude.com/docs/en/about-claude/pricing
//   - Sonnet 4.6 ($3 / $15 per MTok)
//   - Haiku 4.5 ($1 / $5 per MTok)
//   - Opus 4.7 ($5 / $25 per MTok)
//
// SDK NOTE: Sonnet 4.6 and Opus 4.7 have NO Go const in anthropic-sdk-go v1.22.1;
// callers must pass the raw string via `anthropic.Model("claude-sonnet-4-6")`.
// Drop the literal usage at every call site when the SDK ships consts. See
// Phase 24 RESEARCH §Pitfall #4.
var defaultMaxTokensByModel = map[string]int64{
	"claude-sonnet-4-6":           8192,
	"claude-haiku-4-5":            4096,
	"claude-haiku-4-5-20251001":   4096,
	"claude-opus-4-7":             8192,
	"claude-opus-4-6":             8192,
	"claude-sonnet-4-5":           8192,
	"claude-sonnet-4-5-20250929":  8192,
}

// defaultMaxTokensFor returns the MaxTokens default for the given model id.
// Unknown models fall back to 4096 — a safe lower bound that still produces
// usable agent-loop responses for any current Anthropic model.
func defaultMaxTokensFor(model string) int64 {
	if v, ok := defaultMaxTokensByModel[model]; ok {
		return v
	}
	return 4096
}
