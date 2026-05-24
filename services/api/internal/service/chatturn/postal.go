package chatturn

import (
	"context"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// onToolCall handles a tool_call SSE event: persist an AgentTask row in
// PostgreSQL (if AgentTasks dep is wired) and push a "running" entry into
// the user-visible task hub so the dashboard surfaces progress live.
//
// STUB — the full PostalService body will be ported in the next commit.
// Signature is locked-in to match streamOrchestrator's call site.
func (t *Turn) onToolCall(
	ctx context.Context,
	businessID string,
	toolCallID string,
	toolName string,
	toolDisplayName string,
	toolDisplayNameKey string,
	toolArgs map[string]interface{},
	idMap map[string]string,
) {
	_ = ctx
	_ = businessID
	_ = toolCallID
	_ = toolName
	_ = toolDisplayName
	_ = toolDisplayNameKey
	_ = toolArgs
	_ = idMap
}

// onToolResult handles a tool_result SSE event: transition the matching
// AgentTask row to its terminal state and surface the result content on the
// task hub.
//
// STUB — the full PostalService body will be ported in the next commit.
func (t *Turn) onToolResult(
	ctx context.Context,
	businessID string,
	toolCallID string,
	content map[string]interface{},
	toolError string,
	idMap map[string]string,
) {
	_ = ctx
	_ = businessID
	_ = toolCallID
	_ = content
	_ = toolError
	_ = idMap
}

// recordPostsAndReviews fans out post-stream side effects (Posts table /
// Reviews table updates) based on the accumulated tool_call + tool_result
// arrays. Called by Turn.Run AFTER the SSE loop closes.
//
// STUB — the full body will be ported in the next commit.
func (t *Turn) recordPostsAndReviews(
	ctx context.Context,
	businessID string,
	toolCalls []domain.ToolCall,
	toolResults []domain.ToolResult,
) {
	_ = ctx
	_ = businessID
	_ = toolCalls
	_ = toolResults
}
