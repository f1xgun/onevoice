package sse_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// TestMarshal_OmitemptyBytes pins the wire format per event type. These bytes
// are what the orchestrator's chat / resume handlers (and the api's
// HITLCoordinator.ReemitApprovalEvent) must produce after the codec refactor.
// Add a case here when adding a new event-type variant.
func TestMarshal_OmitemptyBytes(t *testing.T) {
	cases := []struct {
		name string
		in   sse.Event
		want string
	}{
		{
			name: "text — only type+content survive",
			in:   sse.Event{Type: "text", Content: "Привет"},
			want: `{"type":"text","content":"Привет"}`,
		},
		{
			name: "done — only type",
			in:   sse.Event{Type: "done"},
			want: `{"type":"done"}`,
		},
		{
			name: "error — type+content",
			in:   sse.Event{Type: "error", Content: "boom"},
			want: `{"type":"error","content":"boom"}`,
		},
		{
			name: "tool_call — args + display key",
			in: sse.Event{
				Type:               "tool_call",
				ToolCallID:         "tc-1",
				ToolName:           "telegram__send_channel_post",
				ToolDisplayName:    "Отправить пост",
				ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
				ToolArgs:           map[string]interface{}{"text": "Hi"},
			},
			want: `{"type":"tool_call","tool_call_id":"tc-1","tool_name":"telegram__send_channel_post","tool_display_name":"Отправить пост","tool_display_name_key":"tools.telegram.send_channel_post.display_name","tool_args":{"text":"Hi"}}`,
		},
		{
			name: "tool_result — result map under `result` key",
			in: sse.Event{
				Type:       "tool_result",
				ToolCallID: "tc-1",
				ToolName:   "telegram__send_channel_post",
				ToolResult: map[string]interface{}{"message_id": float64(123)},
			},
			want: `{"type":"tool_result","tool_call_id":"tc-1","tool_name":"telegram__send_channel_post","result":{"message_id":123}}`,
		},
		{
			name: "tool_rejected — id+name only",
			in: sse.Event{
				Type:       "tool_rejected",
				ToolCallID: "tc-2",
				ToolName:   "yandex_business__post_review_reply",
			},
			want: `{"type":"tool_rejected","tool_call_id":"tc-2","tool_name":"yandex_business__post_review_reply"}`,
		},
		{
			name: "tool_approval_required — typed calls",
			in: sse.Event{
				Type:    "tool_approval_required",
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
			want: `{"type":"tool_approval_required","batch_id":"batch-1","calls":[{"call_id":"tc-3","tool_name":"vk__post_to_wall","args":{"text":"Promo"},"editable_fields":["text"],"floor":"manual"}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := sse.Marshal(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
		})
	}
}

// TestUnmarshal_RoundTrip — Marshal(Unmarshal(x)) == x for every shape the
// orchestrator emits. Catches accidental field renames or json tag changes
// in either direction.
func TestUnmarshal_RoundTrip(t *testing.T) {
	originals := []sse.Event{
		{Type: "text", Content: "Привет"},
		{Type: "done"},
		{Type: "error", Content: "boom"},
		{
			Type:               "tool_call",
			ToolCallID:         "tc-1",
			ToolName:           "telegram__send_channel_post",
			ToolDisplayName:    "Отправить пост",
			ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
			ToolArgs:           map[string]interface{}{"text": "Hi"},
		},
		{
			Type:               "tool_result",
			ToolCallID:         "tc-1",
			ToolName:           "telegram__send_channel_post",
			ToolDisplayName:    "Отправить пост",
			ToolDisplayNameKey: "tools.telegram.send_channel_post.display_name",
			ToolResult:         map[string]interface{}{"message_id": float64(123)},
		},
		{
			Type:    "tool_approval_required",
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
	}

	for i, orig := range originals {
		bytes, err := sse.Marshal(orig)
		require.NoErrorf(t, err, "case %d marshal", i)

		decoded, err := sse.Unmarshal(bytes)
		require.NoErrorf(t, err, "case %d unmarshal", i)

		assert.Equalf(t, orig, decoded, "case %d round-trip mismatch", i)
	}
}

// TestUnmarshal_MalformedJSON — returns a non-nil error and a zero Event when
// the input isn't valid JSON. Callers must check err before reading fields.
func TestUnmarshal_MalformedJSON(t *testing.T) {
	_, err := sse.Unmarshal([]byte(`{type:`))
	require.Error(t, err)
}

// TestMarshal_CodeOmitemptyWhenEmpty — the Code field is absent from the
// marshaled bytes whenever it is the zero string. Pins byte-identity for
// every pre-existing error event that does not set Code.
func TestMarshal_CodeOmitemptyWhenEmpty(t *testing.T) {
	b, err := sse.Marshal(sse.Event{Type: "error", Content: "boom"})
	require.NoError(t, err)
	assert.Equal(t, `{"type":"error","content":"boom"}`, string(b))
	assert.NotContains(t, string(b), `"code"`)
}

// TestMarshal_CodePresentWhenSet — Code marshals as the second key (right after
// type) when populated.
func TestMarshal_CodePresentWhenSet(t *testing.T) {
	b, err := sse.Marshal(sse.Event{
		Type:    "error",
		Code:    "conversation_token_cap",
		Content: "limit reached",
	})
	require.NoError(t, err)
	assert.Equal(t, `{"type":"error","code":"conversation_token_cap","content":"limit reached"}`, string(b))
}

// TestUnmarshal_CodeRoundTrip — Code survives marshal → unmarshal.
func TestUnmarshal_CodeRoundTrip(t *testing.T) {
	orig := sse.Event{Type: "error", Code: "rate_limit_exceeded", Content: "слишком много запросов"}
	bytes, err := sse.Marshal(orig)
	require.NoError(t, err)
	got, err := sse.Unmarshal(bytes)
	require.NoError(t, err)
	assert.Equal(t, orig, got)
}
