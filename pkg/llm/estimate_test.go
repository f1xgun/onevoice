package llm

import (
	"strings"
	"testing"
)

// estimateTokens is unexported, so this test lives in package llm (white-box).

func TestEstimateTokens_Monotonic(t *testing.T) {
	empty := estimateTokens(nil)
	if empty != 0 {
		t.Fatalf("nil messages should estimate 0 tokens, got %d", empty)
	}

	emptySlice := estimateTokens([]Message{})
	if emptySlice != 0 {
		t.Fatalf("empty slice should estimate 0 tokens, got %d", emptySlice)
	}

	short := estimateTokens([]Message{{Role: "user", Content: "hi"}})
	long := estimateTokens([]Message{{Role: "user", Content: strings.Repeat("token ", 1000)}})

	if short <= 0 {
		t.Fatalf("a non-empty message should estimate > 0 tokens, got %d", short)
	}
	if long <= short {
		t.Fatalf("longer content must estimate more tokens: short=%d long=%d", short, long)
	}
}

func TestEstimateTokens_RoughlyCharsOverFour(t *testing.T) {
	content := strings.Repeat("a", 4000) // 4000 bytes ≈ 1000 content tokens
	got := estimateTokens([]Message{{Role: "user", Content: content}})

	if got < 1000 {
		t.Fatalf("4000-char message should estimate >= 1000 tokens (chars/4), got %d", got)
	}
	if got > 1100 {
		t.Fatalf("estimate should stay near chars/4 plus small overhead, got %d", got)
	}
}

func TestEstimateTokens_CountsToolCalls(t *testing.T) {
	withTool := estimateTokens([]Message{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			Function: FunctionCall{Name: "telegram__send_channel_post", Arguments: `{"text":"hello world"}`},
		}},
	}})
	bare := estimateTokens([]Message{{Role: "assistant"}})

	if withTool <= bare {
		t.Fatalf("a message with a tool call must estimate more than a bare one: withTool=%d bare=%d", withTool, bare)
	}
}

// TestEstimateTokens_AccumulatesAcrossMessages — the estimate sums every message
// so a multi-turn prompt costs more than a single turn.
func TestEstimateTokens_AccumulatesAcrossMessages(t *testing.T) {
	one := estimateTokens([]Message{{Role: "user", Content: "hello there friend"}})
	three := estimateTokens([]Message{
		{Role: "user", Content: "hello there friend"},
		{Role: "assistant", Content: "hello there friend"},
		{Role: "user", Content: "hello there friend"},
	})
	if three <= one {
		t.Fatalf("three identical messages must estimate more than one: one=%d three=%d", one, three)
	}
}

// TestEstimateRequestTokens_CountsSystemBlocks — a request with NO messages but a
// large SystemBlock must estimate well above 0. The orchestrator carries the
// system prompt in SystemBlocks (not Messages), so a messages-only estimate would
// return 0 here and let the whole system prompt slip the gate.
func TestEstimateRequestTokens_CountsSystemBlocks(t *testing.T) {
	empty := estimateRequestTokens(ChatRequest{})
	if empty != 0 {
		t.Fatalf("an empty request should estimate 0 tokens, got %d", empty)
	}

	req := ChatRequest{
		SystemBlocks: []SystemBlock{{Text: strings.Repeat("a", 4000)}},
	}
	got := estimateRequestTokens(req)

	if estimateTokens(req.Messages) != 0 {
		t.Fatalf("precondition: messages-only estimate must be 0 for this req, got %d",
			estimateTokens(req.Messages))
	}
	if got < 1000 {
		t.Fatalf("a 4000-char SystemBlock should estimate >= 1000 tokens (chars/4), got %d", got)
	}
}

// TestEstimateRequestTokens_CountsTools — adding a Tool to a request strictly
// increases the estimate (name + description + serialized parameters schema).
func TestEstimateRequestTokens_CountsTools(t *testing.T) {
	base := ChatRequest{
		SystemBlocks: []SystemBlock{{Text: "you are a helpful assistant"}},
	}
	withoutTool := estimateRequestTokens(base)

	base.Tools = []ToolDefinition{{
		Type: ToolCallTypeFunction,
		Function: FunctionDefinition{
			Name:        "telegram__send_channel_post",
			Description: strings.Repeat("send a post to the channel ", 20),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"channel_id": map[string]interface{}{"type": "string", "description": strings.Repeat("x", 200)},
					"text":       map[string]interface{}{"type": "string", "description": strings.Repeat("y", 200)},
				},
				"required": []string{"channel_id", "text"},
			},
		},
	}}
	withTool := estimateRequestTokens(base)

	if withTool <= withoutTool {
		t.Fatalf("adding a Tool must increase the estimate: withoutTool=%d withTool=%d", withoutTool, withTool)
	}
}

// TestEstimateRequestTokens_IncludesMessages — the request estimate is never less
// than the messages-only estimate; SystemBlocks and Tools add on top.
func TestEstimateRequestTokens_IncludesMessages(t *testing.T) {
	req := ChatRequest{
		Messages:     []Message{{Role: "user", Content: strings.Repeat("m", 4000)}},
		SystemBlocks: []SystemBlock{{Text: strings.Repeat("s", 4000)}},
	}
	whole := estimateRequestTokens(req)
	msgsOnly := estimateTokens(req.Messages)

	if whole <= msgsOnly {
		t.Fatalf("request estimate must exceed messages-only when SystemBlocks present: whole=%d msgsOnly=%d",
			whole, msgsOnly)
	}
}

// TestEstimateTokens_CountsRunesNotBytes — the estimate must be language-neutral.
// Cyrillic is 2 UTF-8 bytes per rune, so a byte-length estimator charged a
// Russian prompt twice what it charged the same-length English one, which is
// what pushed ordinary RU chat turns over the per-minute gate.
func TestEstimateTokens_CountsRunesNotBytes(t *testing.T) {
	const runeCount = 4000
	latin := estimateTokens([]Message{{Role: "user", Content: strings.Repeat("a", runeCount)}})
	cyrillic := estimateTokens([]Message{{Role: "user", Content: strings.Repeat("я", runeCount)}})

	if latin != cyrillic {
		t.Fatalf("same rune count must estimate the same: latin=%d cyrillic=%d", latin, cyrillic)
	}
}

// TestEstimateStableTokens_CountsRunesNotBytes — the same language neutrality
// applies to the system prompt, which is Russian in production and dominates
// the per-call estimate.
func TestEstimateStableTokens_CountsRunesNotBytes(t *testing.T) {
	const runeCount = 4000
	latin := estimateStableTokens(ChatRequest{
		SystemBlocks: []SystemBlock{{Text: strings.Repeat("a", runeCount)}},
	})
	cyrillic := estimateStableTokens(ChatRequest{
		SystemBlocks: []SystemBlock{{Text: strings.Repeat("я", runeCount)}},
	})

	if latin != cyrillic {
		t.Fatalf("same rune count must estimate the same: latin=%d cyrillic=%d", latin, cyrillic)
	}
}

// TestEstimateRequestCharge_SplitsStableFromVariable — Messages land in
// Variable, SystemBlocks and Tools in Stable, and Total is their sum. The split
// is what lets the per-minute burst gate stop re-charging the identical prompt
// prefix on every iteration of one agentic turn.
func TestEstimateRequestCharge_SplitsStableFromVariable(t *testing.T) {
	req := ChatRequest{
		Messages:     []Message{{Role: "user", Content: strings.Repeat("m", 4000)}},
		SystemBlocks: []SystemBlock{{Text: strings.Repeat("s", 8000)}},
		Tools: []ToolDefinition{{
			Type: ToolCallTypeFunction,
			Function: FunctionDefinition{
				Name:        "telegram__send_channel_post",
				Description: strings.Repeat("d", 400),
				Parameters:  map[string]interface{}{"type": "object"},
			},
		}},
	}

	charge := estimateRequestCharge(req)

	if charge.Variable != estimateTokens(req.Messages) {
		t.Fatalf("Variable must be the messages-only estimate: got %d want %d",
			charge.Variable, estimateTokens(req.Messages))
	}
	if charge.Stable < 2000 {
		t.Fatalf("an 8000-char SystemBlock plus a tool schema should estimate >= 2000 stable tokens, got %d",
			charge.Stable)
	}
	if charge.Total() != charge.Variable+charge.Stable {
		t.Fatalf("Total must be Variable+Stable: got %d", charge.Total())
	}
	if charge.Total() != estimateRequestTokens(req) {
		t.Fatalf("estimateRequestTokens must equal the charge total: %d vs %d",
			estimateRequestTokens(req), charge.Total())
	}
}

// TestEstimateRequestCharge_EmptyRequest — a request with nothing in it charges
// nothing, so an ungated internal call cannot accidentally consume a window.
func TestEstimateRequestCharge_EmptyRequest(t *testing.T) {
	charge := estimateRequestCharge(ChatRequest{})
	if charge.Variable != 0 || charge.Stable != 0 || charge.Total() != 0 {
		t.Fatalf("empty request must estimate a zero charge, got %+v", charge)
	}
}
