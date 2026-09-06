package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// decodeRejection unmarshals a model-facing rejection tool message.
func decodeRejection(t *testing.T, content string) rejectionPayload {
	t.Helper()
	var got rejectionPayload
	require.NoError(t, json.Unmarshal([]byte(content), &got), "rejection payload must be valid JSON")
	return got
}

// TestBuildRejectionMessage covers CHAT-ORCH-05: every rejection fed back to the
// model names WHO blocked the call and what the model may do next, so it cannot
// invent a platform-side cause and offer a retry that can never succeed.
func TestBuildRejectionMessage(t *testing.T) {
	tests := []struct {
		name   string
		by     rejectionSource
		reason string
		note   string
	}{
		{name: "owner declined on the approval card", by: rejectionByOwner, reason: reasonUserRejected, note: ownerRejectionNote},
		{name: "owner declined with a free-form reason", by: rejectionByOwner, reason: `QA: не "публиковать"`, note: ownerRejectionNote},
		{name: "tool disabled by policy", by: rejectionByPolicy, reason: reasonPolicyForbidden, note: policyForbiddenNote},
		{name: "policy revoked after the card was shown", by: rejectionByPolicy, reason: reasonPolicyRevoked, note: policyRevokedNote},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeRejection(t, buildRejectionMessage(tt.by, tt.reason, tt.note))
			assert.True(t, got.Rejected)
			assert.Equal(t, tt.by, got.By, "the payload must name who blocked the call")
			assert.Equal(t, tt.reason, got.Reason, "the reason must survive verbatim, quotes included")
			assert.Equal(t, tt.note, got.Note, "the note must tell the model what to do next")
			assert.NotEmpty(t, got.Note)
		})
	}
}

// TestStepRun_ForbiddenCallCarriesPolicySemantics asserts the fresh-run
// forbidden path feeds the LLM a self-describing payload rather than a bare
// `policy_forbidden` token.
func TestStepRun_ForbiddenCallCarriesPolicySemantics(t *testing.T) {
	calls := 0
	stub := chatFunc(func(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
		calls++
		if calls == 1 {
			return &llm.ChatResponse{ToolCalls: []llm.ToolCall{{
				ID:       "call-1",
				Type:     llm.ToolCallTypeFunction,
				Function: llm.FunctionCall{Name: "telegram__send_channel_post", Arguments: "{}"},
			}}}, nil
		}
		return &llm.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
	})

	orch := NewWithOptions(stub, toolregistry.NewRegistry(), Options{MaxIterations: 3})
	state := &RunState{Messages: []llm.Message{{Role: "user", Content: "опубликуй"}}}

	out := make(chan Event, 32)
	go func() {
		defer close(out)
		_, _, _ = orch.stepRun(context.Background(), state, out)
	}()
	for range out {
	}

	last := state.Messages[len(state.Messages)-1]
	require.Equal(t, "tool", last.Role)
	got := decodeRejection(t, last.Content)
	assert.Equal(t, rejectionByPolicy, got.By)
	assert.Equal(t, reasonPolicyForbidden, got.Reason)
	assert.Equal(t, policyForbiddenNote, got.Note)
}

// TestDispatchApprovedCalls_RejectionSemantics asserts the three resume-path
// rejection branches (owner verdict, whitelist-withheld tool, policy revoked
// after the card) each feed the LLM the matching source and note.
func TestDispatchApprovedCalls_RejectionSemantics(t *testing.T) {
	const toolName = "telegram__send_channel_post"
	offeredTool := []llm.ToolDefinition{{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: toolName},
	}}

	tests := []struct {
		name       string
		call       domain.PendingCall
		available  []llm.ToolDefinition
		approvals  map[string]domain.ToolFloor
		wantBy     rejectionSource
		wantReason string
		wantNote   string
	}{
		{
			name:       "owner rejected with a free-form reason",
			call:       domain.PendingCall{CallID: "c1", ToolName: "telegram__send_channel_post", Verdict: "reject", RejectReason: "QA: не публиковать"},
			wantBy:     rejectionByOwner,
			wantReason: "QA: не публиковать",
			wantNote:   ownerRejectionNote,
		},
		{
			name:       "owner rejected without a reason",
			call:       domain.PendingCall{CallID: "c2", ToolName: "telegram__send_channel_post", Verdict: "reject"},
			wantBy:     rejectionByOwner,
			wantReason: reasonUserRejected,
			wantNote:   ownerRejectionNote,
		},
		{
			name:       "approved but the tool is no longer offered by the whitelist",
			call:       domain.PendingCall{CallID: "c3", ToolName: toolName, Verdict: "approve"},
			wantBy:     rejectionByPolicy,
			wantReason: reasonPolicyForbidden,
			wantNote:   policyForbiddenNote,
		},
		{
			name:       "approved but the tool policy was revoked after the card",
			call:       domain.PendingCall{CallID: "c4", ToolName: toolName, Verdict: "approve"},
			available:  offeredTool,
			approvals:  map[string]domain.ToolFloor{toolName: domain.ToolFloorForbidden},
			wantBy:     rejectionByPolicy,
			wantReason: reasonPolicyRevoked,
			wantNote:   policyRevokedNote,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := NewWithOptions(chatFunc(nil), toolregistry.NewRegistry(), Options{MaxIterations: 3})
			state := &RunState{AvailableTools: tt.available}
			batch := &domain.PendingToolCallBatch{ID: "b1", Calls: []domain.PendingCall{tt.call}}

			out := make(chan Event, 8)
			done := make(chan struct{})
			go func() {
				defer close(done)
				orch.dispatchApprovedCalls(context.Background(), batch, ResumeRequest{
					BusinessApprovals: tt.approvals,
				}, state, out)
				close(out)
			}()
			for range out {
			}
			<-done

			require.Len(t, state.Messages, 1)
			got := decodeRejection(t, state.Messages[0].Content)
			assert.Equal(t, tt.wantBy, got.By)
			assert.Equal(t, tt.wantReason, got.Reason)
			assert.Equal(t, tt.wantNote, got.Note)
		})
	}
}

// chatFunc adapts a function to LLMClient for the tests above.
type chatFunc func(context.Context, llm.ChatRequest) (*llm.ChatResponse, error)

func (f chatFunc) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return f(ctx, req)
}
