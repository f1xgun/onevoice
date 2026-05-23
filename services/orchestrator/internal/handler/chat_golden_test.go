package handler_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
)

// cannedRunner emits a fixed event sequence regardless of the RunRequest.
// Used by the wire-golden tests so the SSE codec refactor can verify
// byte-identity against the legacy hand-rolled marshalling.
type cannedRunner struct{ events []orchestrator.Event }

func (c *cannedRunner) Run(_ context.Context, _ orchestrator.RunRequest) (<-chan orchestrator.Event, error) {
	ch := make(chan orchestrator.Event, len(c.events))
	for _, e := range c.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// TestChatHandler_WireGolden pins the byte-exact SSE output the orchestrator
// produces for every event type. Refactors of the marshalling path must keep
// this test green so the api proxy + frontend keep parsing identical bytes.
//
// Field ordering in the expected output mirrors the struct's declaration order
// (encoding/json marshals in source order). omitempty fields are absent
// when zero so legacy text/done events stay compact.
func TestChatHandler_WireGolden(t *testing.T) {
	runner := &cannedRunner{events: []orchestrator.Event{
		{Type: orchestrator.EventText, Content: "Привет"},
		{
			Type:               orchestrator.EventToolCall,
			ToolCallID:         "tc-1",
			ToolName:           "telegram__send_channel_post",
			ToolDisplayName:    "Отправить пост",
			ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
			ToolArgs:           map[string]interface{}{"text": "Hi"},
		},
		{
			Type:               orchestrator.EventToolResult,
			ToolCallID:         "tc-1",
			ToolName:           "telegram__send_channel_post",
			ToolDisplayName:    "Отправить пост",
			ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
			ToolResult:         map[string]interface{}{"message_id": float64(123)},
		},
		{
			Type:       orchestrator.EventToolRejected,
			ToolCallID: "tc-2",
			ToolName:   "yandex_business__post_review_reply",
		},
		{
			Type:    orchestrator.EventToolApprovalRequired,
			BatchID: "batch-1",
			Calls: []orchestrator.ApprovalCallSummary{
				{
					CallID:         "tc-3",
					ToolName:       "vk__post_to_wall",
					Args:           map[string]interface{}{"text": "Promo"},
					EditableFields: []string{"text"},
					Floor:          domain.ToolFloorManual,
				},
			},
		},
		{Type: orchestrator.EventDone},
	}}

	h := handler.NewChatHandler(runner, "openai/gpt-4o-mini")

	body := `{"model":"gpt-4o-mini","message":"go","business_id":"biz-1"}`
	req := httptest.NewRequest(http.MethodPost, "/chat/conv-golden", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Chat(w, req)

	expected := strings.Join([]string{
		`data: {"type":"text","content":"Привет"}`,
		``,
		`data: {"type":"tool_call","tool_call_id":"tc-1","tool_name":"telegram__send_channel_post","tool_display_name":"Отправить пост","tool_display_name_key":"tools.telegram.send_channel_post.display_name","tool_args":{"text":"Hi"}}`,
		``,
		`data: {"type":"tool_result","tool_call_id":"tc-1","tool_name":"telegram__send_channel_post","tool_display_name":"Отправить пост","tool_display_name_key":"tools.telegram.send_channel_post.display_name","result":{"message_id":123}}`,
		``,
		`data: {"type":"tool_rejected","tool_call_id":"tc-2","tool_name":"yandex_business__post_review_reply"}`,
		``,
		`data: {"type":"tool_approval_required","batch_id":"batch-1","calls":[{"call_id":"tc-3","tool_name":"vk__post_to_wall","args":{"text":"Promo"},"editable_fields":["text"],"floor":"manual"}]}`,
		``,
		`data: {"type":"done"}`,
		``,
		``,
	}, "\n")

	assert.Equal(t, http.StatusOK, w.Result().StatusCode)
	assert.Equal(t, "text/event-stream", w.Result().Header.Get("Content-Type"))
	assert.Equal(t, expected, w.Body.String(), "SSE wire bytes must remain byte-identical across codec refactors")
}
