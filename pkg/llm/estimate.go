package llm

import (
	"encoding/json"
	"unicode/utf8"
)

// Token-estimate heuristic constants. These are deliberately coarse — the goal
// is a cheap, dependency-free upper-ish bound for the rate-limit gates, NOT a
// model-exact token count.
const (
	// estimateCharsPerToken is the CHARACTERS-per-token divisor. ~4 characters
	// per token is the widely-cited rule of thumb for BPE tokenizers. It is
	// applied to the rune count, never the byte length: Russian is the product's
	// primary language and every Cyrillic rune is 2 UTF-8 bytes, so charging
	// bytes/4 double-counts an entire Russian prompt and lets a per-minute gate
	// sized for real tokens reject ordinary traffic.
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

// TokenCharge splits a pre-flight prompt estimate into the two portions the
// rate-limit gates need to treat differently.
//
// Variable is the part that genuinely differs from call to call — the
// conversation Messages. Stable is the cache-stable prefix every call of a turn
// re-sends unchanged: the system prompt (SystemBlocks) and the tool catalog
// (Tools).
//
// The distinction exists because an agentic turn issues several LLM calls
// within the same fixed minute window (one per tool iteration, plus a HITL
// resume), and each of them re-sends the identical prefix. Accumulating that
// prefix once per call in a per-minute counter makes the burst gate scale with
// the number of iterations rather than with the traffic the user actually
// generated, so a normal multi-step turn exhausts the window. See
// RateLimiter.CheckLimit for how each gate charges the two parts.
type TokenCharge struct {
	Variable int
	Stable   int
}

// Total returns the whole-request estimate — what the month budget is charged
// and what reconcileTokens subtracts from the provider-reported usage.
func (c TokenCharge) Total() int { return c.Variable + c.Stable }

// estimateContentTokens approximates the tokens of a free-text span. Runes, not
// bytes: see estimateCharsPerToken.
func estimateContentTokens(s string) int {
	return utf8.RuneCountInString(s) / estimateCharsPerToken
}

// estimateTokens returns an approximate prompt-token count for the given
// MESSAGES only, used as the Variable half of estimateRequestCharge (the full
// request-level estimator the rate-limit gates actually charge).
//
// It is a DELIBERATE APPROXIMATION, not a model-exact tokenizer: content length
// is divided by estimateCharsPerToken (~4 characters/token, the common BPE rule
// of thumb) and a small fixed per-message overhead plus per-role/name and
// per-tool-call overhead is added. The aim is a cheap, dependency-free volume
// signal for rate limiting. Exact accounting is reconciled post-response from
// the provider's reported usage (see RateLimiter.RecordTokens); this estimate
// only needs to be in the right ballpark to make the gates enforce.
//
// NOTE: this counts Messages ONLY. The orchestrator carries the system prompt in
// ChatRequest.SystemBlocks and the tool catalog in ChatRequest.Tools, neither of
// which lives in Messages — callers gating a whole request must use
// estimateRequestCharge, not this directly.
//
// The returned value is always >= 0 and is monotonic in total content length.
func estimateTokens(messages []Message) int {
	total := 0
	for i := range messages {
		m := &messages[i]
		total += estimatePerMessageOverhead
		total += estimateContentTokens(m.Content)
		total += estimateContentTokens(m.Role)
		for j := range m.ToolCalls {
			total += estimateToolCallOverhead
			total += estimateContentTokens(m.ToolCalls[j].Function.Name)
			total += estimateContentTokens(m.ToolCalls[j].Function.Arguments)
		}
	}
	return total
}

// estimateStableTokens returns an approximate prompt-token count for the
// cache-stable prefix of a request — the system prompt (req.SystemBlocks) plus
// the tool catalog (req.Tools).
//
// The orchestrator (the primary production caller) puts the largest segment,
// the platform+business system prompt, in SystemBlocks and the full tool
// JSON-schema catalog in Tools, both outside Messages. Counting only Messages
// would gate against a large undercount and leave the burst gate structurally
// lenient against a single oversized prompt, so this half is estimated too —
// it is simply charged differently (see TokenCharge). Each SystemBlock
// contributes its text length / estimateCharsPerToken plus a fixed per-block
// overhead; each tool contributes its name, description, and JSON-serialized
// parameters length / estimateCharsPerToken plus a fixed per-schema overhead.
// Nil/empty SystemBlocks and Tools yield zero.
func estimateStableTokens(req ChatRequest) int {
	total := 0

	for i := range req.SystemBlocks {
		total += estimatePerSystemBlockOverhead
		total += estimateContentTokens(req.SystemBlocks[i].Text)
	}

	for i := range req.Tools {
		fn := &req.Tools[i].Function
		total += estimatePerToolSchemaOverhead
		total += estimateContentTokens(fn.Name)
		total += estimateContentTokens(fn.Description)
		if fn.Parameters != nil {
			if schema, err := json.Marshal(fn.Parameters); err == nil {
				total += estimateContentTokens(string(schema))
			}
		}
	}

	return total
}

// estimateRequestCharge returns the pre-flight TokenCharge for a whole request:
// Messages as the Variable half, SystemBlocks+Tools as the Stable half.
//
// Both checkRateLimit and reconcileTokens call this on the same req so the
// charged estimate and the reconcile baseline agree.
func estimateRequestCharge(req ChatRequest) TokenCharge {
	return TokenCharge{
		Variable: estimateTokens(req.Messages),
		Stable:   estimateStableTokens(req),
	}
}

// estimateRequestTokens returns the whole-request estimate (Variable+Stable) as
// a single number.
func estimateRequestTokens(req ChatRequest) int {
	return estimateRequestCharge(req).Total()
}
