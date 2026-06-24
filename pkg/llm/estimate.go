package llm

import "encoding/json"

// Token-estimate heuristic constants. These are deliberately coarse — the goal
// is a cheap, dependency-free upper-ish bound for the rate-limit gates, NOT a
// model-exact token count.
const (
	// estimateCharsPerToken is the bytes-per-token divisor. ~4 bytes/token is the
	// widely-cited rule of thumb for English BPE tokenizers; it under-counts for
	// CJK and over-counts for whitespace-heavy text, which is acceptable for a
	// volume gate.
	estimateCharsPerToken = 4

	// estimatePerMessageOverhead approximates the fixed structural tokens every
	// chat message carries on the wire (role delimiters, message framing). Real
	// chat templates spend a handful of tokens per message regardless of content.
	estimatePerMessageOverhead = 4

	// estimateToolCallOverhead is added per tool call attached to a message, plus
	// the argument JSON length is counted as content. Tool-call framing (id, type,
	// function name wrapper) costs a few tokens beyond the raw arguments.
	estimateToolCallOverhead = 4

	// estimatePerSystemBlockOverhead approximates the fixed structural tokens a
	// system block carries on the wire (block framing / role delimiter), mirroring
	// estimatePerMessageOverhead for the SystemBlocks channel.
	estimatePerSystemBlockOverhead = 4

	// estimatePerToolSchemaOverhead approximates the fixed framing tokens each tool
	// definition carries beyond its name/description/parameters (the function
	// wrapper, "type":"function", JSON-schema scaffolding).
	estimatePerToolSchemaOverhead = 4
)

// estimateTokens returns an approximate prompt-token count for the given
// MESSAGES only, used as the per-message building block of estimateRequestTokens
// (the full request-level estimator the rate-limit gates actually charge).
//
// It is a DELIBERATE APPROXIMATION, not a model-exact tokenizer: content length
// is divided by estimateCharsPerToken (~4 bytes/token, the common BPE rule of
// thumb) and a small fixed per-message overhead plus per-role/name and
// per-tool-call overhead is added. The aim is a cheap, dependency-free volume
// signal for rate limiting. Exact accounting is reconciled post-response from
// the provider's reported usage (see RateLimiter.RecordTokens); this estimate
// only needs to be in the right ballpark to make the gates enforce.
//
// NOTE: this counts Messages ONLY. The orchestrator carries the system prompt in
// ChatRequest.SystemBlocks and the tool catalog in ChatRequest.Tools, neither of
// which lives in Messages — callers gating a whole request must use
// estimateRequestTokens, not this directly.
//
// The returned value is always >= 0 and is monotonic in total content length.
func estimateTokens(messages []Message) int {
	total := 0
	for i := range messages {
		m := &messages[i]
		total += estimatePerMessageOverhead
		total += len(m.Content) / estimateCharsPerToken
		total += len(m.Role) / estimateCharsPerToken
		for j := range m.ToolCalls {
			total += estimateToolCallOverhead
			total += len(m.ToolCalls[j].Function.Name) / estimateCharsPerToken
			total += len(m.ToolCalls[j].Function.Arguments) / estimateCharsPerToken
		}
	}
	return total
}

// estimateRequestTokens returns an approximate prompt-token count for the WHOLE
// request — Messages plus the system prompt (req.SystemBlocks) plus the tool
// catalog (req.Tools) — and is what the rate-limit gates charge pre-flight.
//
// estimateTokens alone walks Messages only, but the orchestrator (the primary
// production caller) puts the largest segment, the platform+business system
// prompt, in SystemBlocks and the full tool JSON-schema catalog in Tools, both
// outside Messages. Counting only Messages would gate against a large undercount
// and leave the per-minute burst gate structurally lenient, so this wrapper adds
// both. Each SystemBlock contributes its text length / estimateCharsPerToken plus
// a fixed per-block overhead; each tool contributes its name, description, and
// JSON-serialized parameters length / estimateCharsPerToken plus a fixed
// per-schema overhead. Nil/empty SystemBlocks and Tools add zero.
//
// Same heuristic as estimateTokens (chars/4 + small per-segment overhead); it is
// an approximation, not a model-exact count. Both checkRateLimit and
// reconcileTokens call this on the same req so the charged estimate and the
// reconcile baseline agree.
func estimateRequestTokens(req ChatRequest) int {
	total := estimateTokens(req.Messages)

	for i := range req.SystemBlocks {
		total += estimatePerSystemBlockOverhead
		total += len(req.SystemBlocks[i].Text) / estimateCharsPerToken
	}

	for i := range req.Tools {
		fn := &req.Tools[i].Function
		total += estimatePerToolSchemaOverhead
		total += len(fn.Name) / estimateCharsPerToken
		total += len(fn.Description) / estimateCharsPerToken
		if fn.Parameters != nil {
			if schema, err := json.Marshal(fn.Parameters); err == nil {
				total += len(schema) / estimateCharsPerToken
			}
		}
	}

	return total
}
