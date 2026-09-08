package chatturn

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestFinalizeStranded_PreservesApprovalEvidence(t *testing.T) {
	message := &domain.Message{
		ID: "stranded-message", ConversationID: "conversation",
		Role: domain.MessageRoleAssistant, Status: domain.MessageStatusPendingApproval,
		ToolCalls: []domain.ToolCall{
			{ID: "unknown", Status: domain.ToolCallStatusPending},
			{ID: "approved", Status: domain.ToolCallStatusApproved},
			{ID: "rejected", Status: domain.ToolCallStatusRejected},
		},
		ToolResults: []domain.ToolResult{{ToolCallID: "approved", Content: map[string]interface{}{"ok": true}}},
	}
	repo := &resumeMsgRepo{active: message}
	turn := &Turn{deps: Deps{Messages: repo, Conversations: resumeStubConv{}}}

	turn.finalizeStranded(context.Background(), message)

	require.NotNil(t, repo.updated)
	assert.Equal(t, domain.MessageStatusComplete, repo.updated.Status)
	assert.Equal(t, message.ToolCalls, repo.updated.ToolCalls, "recovery must not invent an approval for a missing batch")
	assert.Equal(t, message.ToolResults, repo.updated.ToolResults)
	assert.Equal(t, domain.MessageStatusPendingApproval, message.Status, "the caller snapshot is unchanged")
}
