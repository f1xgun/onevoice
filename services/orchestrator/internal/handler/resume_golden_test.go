package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
)

// TestResumeHandler_WireGolden pins the byte-exact SSE output of the resume
// path. The chat_golden_test does the same for /chat — the two together
// anchor every codec-touching field across both emit paths.
func TestResumeHandler_WireGolden(t *testing.T) {
	resumer := &stubResumer{fn: func(_ context.Context, _ orchestrator.ResumeRequest) (<-chan orchestrator.Event, error) {
		ch := make(chan orchestrator.Event, 6)
		ch <- orchestrator.Event{
			Type:               orchestrator.EventToolCall,
			ToolCallID:         "tc-1",
			ToolName:           "telegram__send_channel_post",
			ToolDisplayName:    "Отправить пост",
			ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
			ToolArgs:           map[string]interface{}{"text": "Hi"},
		}
		ch <- orchestrator.Event{
			Type:               orchestrator.EventToolResult,
			ToolCallID:         "tc-1",
			ToolName:           "telegram__send_channel_post",
			ToolDisplayName:    "Отправить пост",
			ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
			ToolResult:         map[string]interface{}{"message_id": float64(123)},
		}
		ch <- orchestrator.Event{
			Type:       orchestrator.EventToolRejected,
			ToolCallID: "tc-2",
			ToolName:   "yandex_business__post_review_reply",
		}
		ch <- orchestrator.Event{
			Type:    orchestrator.EventToolApprovalRequired,
			BatchID: "batch-2",
			Calls: []sse.ApprovalCall{
				{
					CallID:         "tc-3",
					ToolName:       "vk__post_to_wall",
					Args:           map[string]interface{}{"text": "Promo"},
					EditableFields: []string{"text"},
					Floor:          domain.ToolFloorManual,
				},
			},
		}
		ch <- orchestrator.Event{Type: orchestrator.EventDone}
		close(ch)
		return ch, nil
	}}

	h := handler.NewResumeHandler(resumer, "")
	req := httptest.NewRequest(http.MethodPost, "/chat/conv1/resume?batch_id=batch-2", http.NoBody)
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	expected := strings.Join([]string{
		`data: {"type":"tool_call","tool_call_id":"tc-1","tool_name":"telegram__send_channel_post","tool_display_name":"Отправить пост","tool_display_name_key":"tools.telegram.send_channel_post.display_name","tool_args":{"text":"Hi"}}`,
		``,
		`data: {"type":"tool_result","tool_call_id":"tc-1","tool_name":"telegram__send_channel_post","tool_display_name":"Отправить пост","tool_display_name_key":"tools.telegram.send_channel_post.display_name","result":{"message_id":123}}`,
		``,
		`data: {"type":"tool_rejected","tool_call_id":"tc-2","tool_name":"yandex_business__post_review_reply"}`,
		``,
		`data: {"type":"tool_approval_required","batch_id":"batch-2","calls":[{"call_id":"tc-3","tool_name":"vk__post_to_wall","args":{"text":"Promo"},"editable_fields":["text"],"floor":"manual"}]}`,
		``,
		`data: {"type":"done"}`,
		``,
		``,
	}, "\n")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, expected, rec.Body.String(), "resume SSE wire bytes must remain byte-identical across codec refactors")
}
