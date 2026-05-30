package providers

// Per-model context-window sizes (in tokens) for the current Claude generation.
const (
	claudeSonnet4_6ContextLength = 1_000_000
	claudeHaiku4_5ContextLength  = 200_000
	claudeOpus4_7ContextLength   = 1_000_000
)

// Per-model MaxTokens defaults (max output tokens).
//
// Anthropic returns HTTP 400 if `max_tokens` is missing, so we always send a
// non-zero value. 8192 / 4096 are well below each model's per-model output cap
// (64k–128k) — conservative defaults that keep cost predictable.
const (
	maxTokensSonnetDefault  int64 = 8192
	maxTokensHaikuDefault   int64 = 4096
	maxTokensOpusDefault    int64 = 8192
	maxTokensUnknownDefault int64 = 4096
)

// defaultMaxTokensByModel resolves `MaxTokens` defaults that we send to the
// Anthropic API when the caller leaves `ChatRequest.MaxTokens=0`.
//
// PRICING NOTE: as of 2026-05-30 per platform.claude.com/docs/en/about-claude/pricing
//   - Sonnet 4.6 ($3 / $15 per MTok)
//   - Haiku 4.5 ($1 / $5 per MTok)
//   - Opus 4.7 ($5 / $25 per MTok)
//
// SDK NOTE: Sonnet 4.6 and Opus 4.7 have NO Go const in anthropic-sdk-go v1.22.1;
// callers must pass the raw string via `anthropic.Model("claude-sonnet-4-6")`.
// Drop the literal usage at every call site when the SDK ships consts.
var defaultMaxTokensByModel = map[string]int64{
	"claude-sonnet-4-6":          maxTokensSonnetDefault,
	"claude-haiku-4-5":           maxTokensHaikuDefault,
	"claude-haiku-4-5-20251001":  maxTokensHaikuDefault,
	"claude-opus-4-7":            maxTokensOpusDefault,
	"claude-opus-4-6":            maxTokensOpusDefault,
	"claude-sonnet-4-5":          maxTokensSonnetDefault,
	"claude-sonnet-4-5-20250929": maxTokensSonnetDefault,
}

// defaultMaxTokensFor returns the MaxTokens default for the given model id.
// Unknown models fall back to `maxTokensUnknownDefault` — a safe lower bound
// that still produces usable agent-loop responses for any current Anthropic
// model.
func defaultMaxTokensFor(model string) int64 {
	if v, ok := defaultMaxTokensByModel[model]; ok {
		return v
	}
	return maxTokensUnknownDefault
}
