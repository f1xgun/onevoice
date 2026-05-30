// Package sseevent owns the orchestrator-internal mapping from
// orchestrator.Event to the wire-level pkg/sse.Event. The mapping was
// previously duplicated as a 30-line switch in handler/chat.go and
// handler/resume.go; it lives here so a new field on either side is one edit.
//
// This package is orchestrator-internal because it imports the orchestrator's
// EventType enum. The wire shape it produces (pkg/sse.Event) is the contract
// the api decodes against — but the api never imports this builder.
package sseevent

import (
	"github.com/f1xgun/onevoice/pkg/sse"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
)

// FromEvent projects an orchestrator agent-loop event into the canonical SSE
// wire shape. Field copying is gated by event type so omitempty fields stay
// absent for legacy event variants (text / done / error keep their compact
// byte form).
func FromEvent(e orchestrator.Event) sse.Event {
	out := sse.Event{
		Type:    string(e.Type),
		Code:    e.Code,
		Content: e.Content,
	}
	switch e.Type {
	case orchestrator.EventToolCall:
		out.ToolCallID = e.ToolCallID
		out.ToolName = e.ToolName
		out.ToolDisplayName = e.ToolDisplayName
		out.ToolDisplayNameKey = e.ToolDisplayNameKey
		out.ToolArgs = e.ToolArgs
	case orchestrator.EventToolResult:
		out.ToolCallID = e.ToolCallID
		out.ToolName = e.ToolName
		out.ToolDisplayName = e.ToolDisplayName
		out.ToolDisplayNameKey = e.ToolDisplayNameKey
		out.ToolResult = e.ToolResult
		out.ToolError = e.ToolError
	case orchestrator.EventToolRejected:
		// policy_forbidden / policy_revoked / user_rejected. Carries only
		// id+name; the chat_proxy / FE renders the rejection from those.
		out.ToolCallID = e.ToolCallID
		out.ToolName = e.ToolName
	case orchestrator.EventToolApprovalRequired:
		// One pause event per turn carrying all manual-floor calls.
		out.BatchID = e.BatchID
		out.Calls = e.Calls
	case orchestrator.EventText, orchestrator.EventError, orchestrator.EventDone:
		// No additional fields beyond Type + Content.
	}
	return out
}
