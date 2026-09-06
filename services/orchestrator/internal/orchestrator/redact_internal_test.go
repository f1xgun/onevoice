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

	redactRequestPDn(&req, nil)

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
	redactRequestPDn(&req, nil)
	once := req.Messages[0].Content
	redactRequestPDn(&req, nil)
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
	redactRequestPDn(&req, nil)
	assert.Equal(t, "Заказ 1234567890 готов", req.Messages[0].Content,
		"a bare order number must not trip the phone/cc heuristics")
	assert.Equal(t, "## Бизнес: Кофейня «Утро»", req.SystemBlocks[1].Text)
}

// TestRedactRequestPDn_Allowlist covers CHAT-ORCH-03: values on the allowlist
// (the business's own registered contacts) survive every scrubbed surface —
// message content, tool-call arguments and the business prompt block — in any
// spelling, while everything else is still redacted.
func TestRedactRequestPDn_Allowlist(t *testing.T) {
	const ownPhone = "+7 (843) 555-12-34"

	tests := []struct {
		name       string
		allow      []string
		content    string
		wantKeep   []string
		wantRedact []string
	}{
		{
			name:       "own phone survives in the exact registered spelling",
			allow:      []string{ownPhone},
			content:    "Звоните: +7 (843) 555-12-34",
			wantKeep:   []string{"+7 (843) 555-12-34"},
			wantRedact: nil,
		},
		{
			name:       "own phone survives in the 8-prefixed national spelling",
			allow:      []string{ownPhone},
			content:    "Наш телефон 8 843 555-12-34",
			wantKeep:   []string{"8 843 555-12-34"},
			wantRedact: nil,
		},
		{
			name:       "own phone survives in E.164 spelling",
			allow:      []string{ownPhone},
			content:    "тел. +78435551234",
			wantKeep:   []string{"+78435551234"},
			wantRedact: nil,
		},
		{
			name:       "a third-party phone alongside the allowed one is still redacted",
			allow:      []string{ownPhone},
			content:    "наш +78435551234, клиента +78120000000",
			wantKeep:   []string{"+78435551234"},
			wantRedact: []string{"+78120000000"},
		},
		{
			name:       "allowed email survives, other emails do not",
			allow:      []string{"Info@Utro.ru"},
			content:    "пишите info@utro.ru, клиент писал с a@b.com",
			wantKeep:   []string{"info@utro.ru"},
			wantRedact: []string{"a@b.com"},
		},
		{
			name:       "empty allowlist redacts everything (RedactPII parity)",
			allow:      nil,
			content:    "наш +78435551234",
			wantKeep:   nil,
			wantRedact: []string{"+78435551234"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := llm.ChatRequest{
				Messages: []llm.Message{
					{Role: "user", Content: tt.content},
					{Role: "assistant", ToolCalls: []llm.ToolCall{{
						ID:   "c1",
						Type: llm.ToolCallTypeFunction,
						Function: llm.FunctionCall{
							Name:      "telegram__send_channel_post",
							Arguments: `{"text":"` + tt.content + `"}`,
						},
					}}},
				},
				SystemBlocks: []llm.SystemBlock{
					{Text: "platform prefix", CacheBoundary: true},
					{Text: tt.content, CacheBoundary: false},
				},
			}

			redactRequestPDn(&req, tt.allow)

			surfaces := map[string]string{
				"message content":     req.Messages[0].Content,
				"tool-call arguments": req.Messages[1].ToolCalls[0].Function.Arguments,
				"business block":      req.SystemBlocks[1].Text,
			}
			for label, got := range surfaces {
				for _, keep := range tt.wantKeep {
					assert.Contains(t, got, keep, "%s must keep the allowlisted value", label)
				}
				for _, gone := range tt.wantRedact {
					assert.NotContains(t, got, gone, "%s must redact the third-party value", label)
				}
			}
			assert.Equal(t, "platform prefix", req.SystemBlocks[0].Text)
		})
	}
}

func TestPublicationContactAllowlist(t *testing.T) {
	for _, tt := range []struct {
		name, role, text string
		want             bool
	}{
		{"booking", "user", "Сделай пост… запись по телефону +7 916 123-45-77", true},
		{"formatted", "user", "Добавь в пост телефон: +7 (916) 123-45-77", true},
		{"trunk", "user", "Телефон для публикации: 8 916 123 45 77", true},
		{"english", "user", "Write a post. Booking phone: +79161234577", true},
		{"unscoped", "user", "Позвони +79161234577", false},
		{"negative", "user", "Не добавь в пост телефон +79161234577", false},
		{"customer", "user", "Отзыв клиента: запись по телефону +79161234577", false},
		{"quoted", "user", "Он написал: «запись по телефону +79161234577»", false},
		{"tool", "tool", "Запись по телефону +79161234577", false},
		{"assistant", "assistant", "Запись по телефону +79161234577", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			allow := publicationContactAllowlist([]llm.Message{{Role: tt.role, Content: tt.text}})
			req := llm.ChatRequest{Messages: []llm.Message{{Role: "assistant", Content: "+7 (916) 123-45-77; +78120000000; person@example.org"}}}
			redactRequestPDn(&req, allow)
			if tt.want {
				assert.Contains(t, req.Messages[0].Content, "+7 (916) 123-45-77")
			} else {
				assert.NotContains(t, req.Messages[0].Content, "916")
			}
			assert.NotContains(t, req.Messages[0].Content, "+78120000000")
			assert.NotContains(t, req.Messages[0].Content, "person@example.org")
		})
	}
	assert.Empty(t, publicationContactAllowlist([]llm.Message{
		{Role: "user", Content: "Запись по телефону +79161234577"},
		{Role: "user", Content: "Напиши другой пост без контактов"},
	}))
}
