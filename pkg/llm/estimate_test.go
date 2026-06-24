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
