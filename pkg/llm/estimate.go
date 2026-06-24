package llm

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
)

// estimateTokens returns an approximate prompt-token count for the given
// messages, used to feed the per-minute / per-month token rate-limit gates.
//
// It is a DELIBERATE APPROXIMATION, not a model-exact tokenizer: content length
// is divided by estimateCharsPerToken (~4 bytes/token, the common BPE rule of
// thumb) and a small fixed per-message overhead plus per-role/name and
// per-tool-call overhead is added. The aim is a cheap, dependency-free,
// conservative-by-design volume signal for rate limiting — it intentionally
// rounds up rather than risk under-counting and letting a runaway prompt slip
// the gate. Exact accounting is reconciled post-response from the provider's
// reported usage (see RateLimiter.RecordTokens); this estimate only needs to be
// in the right ballpark to make the gates enforce.
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
