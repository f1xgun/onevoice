package sseevent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/sseevent"
)

// TestFromEvent_PerType pins which fields cross the seam per event type. Fields
// not listed in the case's wantHas set must stay at zero so the wire output
// keeps its omitempty compactness.
func TestFromEvent_PerType(t *testing.T) {
	full := orchestrator.Event{
		Type:               orchestrator.EventText,
		Content:            "Привет",
		ToolCallID:         "tc-1",
		ToolName:           "telegram__send_channel_post",
		ToolDisplayName:    "Отправить пост",
		ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
		ToolArgs:           map[string]interface{}{"text": "Hi"},
		ToolResult:         map[string]interface{}{"message_id": float64(123)},
		ToolError:          "boom",
		BatchID:            "batch-1",
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

	cases := []struct {
		name string
		typ  orchestrator.EventType
		want sse.Event
	}{
		{
			name: "text — only type+content",
			typ:  orchestrator.EventText,
			want: sse.Event{Type: "text", Content: "Привет"},
		},
		{
			name: "done — only type",
			typ:  orchestrator.EventDone,
			want: sse.Event{Type: "done", Content: "Привет"},
		},
		{
			name: "error — type+content",
			typ:  orchestrator.EventError,
			want: sse.Event{Type: "error", Content: "Привет"},
		},
		{
			name: "tool_call — id, name, display, key, args",
			typ:  orchestrator.EventToolCall,
			want: sse.Event{
				Type:               "tool_call",
				Content:            "Привет",
				ToolCallID:         "tc-1",
				ToolName:           "telegram__send_channel_post",
				ToolDisplayName:    "Отправить пост",
				ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
				ToolArgs:           map[string]interface{}{"text": "Hi"},
			},
		},
		{
			name: "tool_result — id, name, display, key, result, error",
			typ:  orchestrator.EventToolResult,
			want: sse.Event{
				Type:               "tool_result",
				Content:            "Привет",
				ToolCallID:         "tc-1",
				ToolName:           "telegram__send_channel_post",
				ToolDisplayName:    "Отправить пост",
				ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
				ToolResult:         map[string]interface{}{"message_id": float64(123)},
				ToolError:          "boom",
			},
		},
		{
			name: "tool_rejected — id+name only",
			typ:  orchestrator.EventToolRejected,
			want: sse.Event{
				Type:       "tool_rejected",
				Content:    "Привет",
				ToolCallID: "tc-1",
				ToolName:   "telegram__send_channel_post",
			},
		},
		{
			name: "tool_approval_required — batch+calls only",
			typ:  orchestrator.EventToolApprovalRequired,
			want: sse.Event{
				Type:    "tool_approval_required",
				Content: "Привет",
				BatchID: "batch-1",
				Calls: []sse.ApprovalCall{
					{
						CallID:         "tc-3",
						ToolName:       "vk__post_to_wall",
						Args:           map[string]interface{}{"text": "Promo"},
						EditableFields: []string{"text"},
						Floor:          domain.ToolFloorManual,
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := full
			input.Type = tc.typ
			got := sseevent.FromEvent(input)
			assert.Equal(t, tc.want, got)
		})
	}
}
