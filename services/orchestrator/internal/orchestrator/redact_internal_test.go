package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// TestRedactRequestPDn_ScrubsContentToolCallsAndBusinessBlock asserts the
// transform redacts message content, tool-call arguments, and per-business
// system blocks while leaving block 0 (the platform prefix) byte-identical.
func TestRedactRequestPDn_ScrubsContentToolCallsAndBusinessBlock(t *testing.T) {
	original := []llm.Message{
		{Role: "user", Content: "позвони на +79991234567"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID:   "c1",
			Type: llm.ToolCallTypeFunction,
			Function: llm.FunctionCall{
				Name:      "telegram__send_channel_post",
				Arguments: `{"text":"пишите на a@b.com"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: `{"author_phone":"+7 999 123-45-67"}`},
	}
	req := llm.ChatRequest{
		Messages: original,
		SystemBlocks: []llm.SystemBlock{
			{Text: "platform prefix without pii", CacheBoundary: true},
			{Text: "Телефон: +79991234567", CacheBoundary: false},
		},
	}

	redactRequestPDn(&req)

	require.Len(t, req.Messages, 3)
	assert.Contains(t, req.Messages[0].Content, "[Скрыто]")
	assert.NotContains(t, req.Messages[0].Content, "+79991234567")
	require.Len(t, req.Messages[1].ToolCalls, 1)
	assert.Contains(t, req.Messages[1].ToolCalls[0].Function.Arguments, "[Скрыто]")
	assert.NotContains(t, req.Messages[1].ToolCalls[0].Function.Arguments, "a@b.com")
	assert.Contains(t, req.Messages[2].Content, "[Скрыто]")

	assert.Equal(t, "platform prefix without pii", req.SystemBlocks[0].Text,
		"block 0 (platform prefix) must never be redacted")
	assert.True(t, req.SystemBlocks[0].CacheBoundary)
	assert.Contains(t, req.SystemBlocks[1].Text, "[Скрыто]")
	assert.NotContains(t, req.SystemBlocks[1].Text, "+79991234567")

	assert.Equal(t, "позвони на +79991234567", original[0].Content,
		"redaction must not mutate the caller's Messages backing array")
	assert.Equal(t, `{"text":"пишите на a@b.com"}`, original[1].ToolCalls[0].Function.Arguments,
		"redaction must not mutate the caller's tool-call arguments")
	assert.Equal(t, `{"author_phone":"+7 999 123-45-67"}`, original[2].Content,
		"redaction must not mutate the caller's tool-result message content")
}

// TestRedactRequestPDn_Idempotent asserts a second pass is a no-op (the
// placeholder is not itself PII).
func TestRedactRequestPDn_Idempotent(t *testing.T) {
	req := llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "почта a@b.com"}}}
	redactRequestPDn(&req)
	once := req.Messages[0].Content
	redactRequestPDn(&req)
	assert.Equal(t, once, req.Messages[0].Content)
}

// TestRedactRequestPDn_PreservesNonPII asserts plain business text and Cyrillic
// names survive untouched.
func TestRedactRequestPDn_PreservesNonPII(t *testing.T) {
	req := llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Заказ 1234567890 готов"}},
		SystemBlocks: []llm.SystemBlock{
			{Text: "platform", CacheBoundary: true},
			{Text: "## Бизнес: Кофейня «Утро»", CacheBoundary: false},
		},
	}
	redactRequestPDn(&req)
	assert.Equal(t, "Заказ 1234567890 готов", req.Messages[0].Content,
		"a bare order number must not trip the phone/cc heuristics")
	assert.Equal(t, "## Бизнес: Кофейня «Утро»", req.SystemBlocks[1].Text)
}
