package chatturn

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// streamBudget caps a single chat stream's wall-clock duration. Generous
// enough that a long RPA tool chain can finish; tight enough that a stuck
// agent can't pin the connection forever.
const streamBudget = 10 * time.Minute

// StreamBudget exposes streamBudget to the wiring layer so the SSE
// concurrency counter can size its slot-key TTL to outlive a single stream
// (ssecounter.NewWithKeyTTL). Keeping it derived from the same constant means
// the two cannot drift apart.
const StreamBudget = streamBudget

// streamState is per-request mutable state populated by the SSE event loop.
// Owned by Run on its stack; passed by pointer to the per-event handlers so
// they can mutate it.
type streamState struct {
	assistantText    strings.Builder
	toolCalls        []domain.ToolCall
	toolResults      []domain.ToolResult
	pauseEvent       *sse.Event
	streamErrContent string
	streamErrCode    string
	// idMap propagates LLM tool_call.id -> internal agent_task.id mapping so
	// tool_result events can correlate even when the orchestrator's
	// tool_call.id and the dispatched agent task have different identifiers.
	idMap map[string]string
}

// toolArgsByCallID returns the arguments of the tool_call that matches a
// tool_result frame, or nil when no matching call was seen. The tool_call always
// arrives before its tool_result, so by the time a result is dispatched its
// call is already recorded in toolCalls.
func (s *streamState) toolArgsByCallID(toolCallID string) map[string]interface{} {
	if toolCallID == "" {
		return nil
	}
	for i := range s.toolCalls {
		if s.toolCalls[i].ID == toolCallID {
			return s.toolCalls[i].Arguments
		}
	}
	return nil
}

// streamOrchestrator opens POST /chat/{id} on the orchestrator, forwards SSE
// bytes verbatim to w, and routes each parsed event into the per-stream
// state + on-the-fly postal side effects.
//
// parentCtx supplies the correlation_id and the client-disconnect signal;
// the orchestrator request runs on a detached 10-minute context (via
// StreamSSEOptions.OrchCtxBudget) so a disconnect does NOT abort the upstream
// — AgentTask rows still reach terminal states even after the user navigates
// away. The SSE plumbing (detached ctx, correlation propagation, response
// headers, scanner with 1MiB buffer, clientGone-aware drain loop) lives in
// pkg/orchestratorclient.StreamSSE; this function owns ONLY the per-event
// domain dispatch.
func (t *Turn) streamOrchestrator(
	parentCtx context.Context,
	taskOpsCtx context.Context,
	w http.ResponseWriter,
	conversationID string,
	body []byte,
	headers map[string]string,
	businessID string,
	state *streamState,
	emit func(sse.Event),
) error {
	return t.deps.Orch.StreamSSE(parentCtx, orchestratorclient.StreamSSERequest{
		ConversationID: conversationID,
		Body:           body,
		Writer:         w,
		Headers:        headers,
		OrchCtxBudget:  streamBudget,
		OnEvent: func(ev sse.Event) {
			t.dispatchEvent(taskOpsCtx, businessID, state, ev)
			if emit != nil {
				emit(ev)
			}
		},
	})
}

// dispatchEvent routes a single parsed SSE frame into the per-stream state
// and triggers the on-the-fly postal side effects (AgentTask creation /
// terminal-state writes happen mid-stream, not after, so the user sees
// progress live).
func (t *Turn) dispatchEvent(taskOpsCtx context.Context, businessID string, state *streamState, ev sse.Event) {
	switch ev.Type {
	case "text":
		state.assistantText.WriteString(ev.Content)
	case "tool_call":
		state.toolCalls = append(state.toolCalls, domain.ToolCall{
			ID:        ev.ToolCallID,
			Name:      ev.ToolName,
			Arguments: ev.ToolArgs,
		})
		t.onToolCall(taskOpsCtx, businessID, ev.ToolCallID, ev.ToolName, ev.ToolDisplayName, ev.ToolDisplayNameKey, ev.ToolArgs, "", state.idMap)
	case "tool_result":
		var content map[string]interface{}
		if m, ok := ev.ToolResult.(map[string]interface{}); ok {
			content = m
		} else {
			content = map[string]interface{}{"raw": ev.ToolResult}
		}
		state.toolResults = append(state.toolResults, domain.ToolResult{
			ToolCallID: ev.ToolCallID,
			Content:    content,
			IsError:    ev.ToolError != "",
			Code:       ev.Code,
		})
		toolArgs := state.toolArgsByCallID(ev.ToolCallID)
		t.onToolResult(taskOpsCtx, businessID, ev.ToolCallID, content, toolArgs, ev.ToolError, ev.Code, state.idMap)
	case "tool_approval_required":
		evCopy := ev
		state.pauseEvent = &evCopy
	case "error":
		state.streamErrContent = ev.Content
		state.streamErrCode = ev.Code
		if state.streamErrCode == "" {
			state.streamErrCode = "STREAM_ERROR"
		}
	}
}

// buildOrchestratorRequest assembles the JSON body forwarded to /chat/{id}.
//
// The rate-limit tier is resolved per-business through PlanResolver (fail-safe:
// DB error / no subscription → Free) instead of the legacy hardcoded empty
// string. This is behavior-preserving today — with no subscriptions every
// business resolves to Free (== the orchestrator's ""→"free" default) — while
// making per-business tiering correct once Track-B creates subscriptions. A nil
// PlanResolver (struct-literal test path) forwards the byte-identical legacy
// empty tier.
func (t *Turn) buildOrchestratorRequest(ctx context.Context, req TurnRequest, enriched *enrichmentResult) map[string]interface{} {
	business := enriched.business
	tier := ""
	if t.deps.PlanResolver != nil {
		tier = t.deps.PlanResolver.Resolve(ctx, business.ID).RateLimitTier
	}
	return map[string]interface{}{
		"model":                      req.Model,
		"message":                    req.Message,
		"business_id":                business.ID.String(),
		"business_name":              business.Name,
		"business_category":          business.Category,
		"business_address":           business.Address,
		"business_phone":             business.Phone,
		"business_website":           derefString(business.Website),
		"business_description":       business.Description,
		"business_voice_tone":        extractVoiceTone(business.Settings),
		"business_voice_profile":     extractVoiceProfile(business.Settings),
		"active_integrations":        enriched.activeIntegrations,
		"history":                    enriched.history,
		"project_id":                 enriched.project.id,
		"project_name":               enriched.project.name,
		"project_system_prompt":      enriched.project.systemPrompt,
		"project_whitelist_mode":     enriched.project.whitelistMode,
		"project_allowed_tools":      enriched.project.allowedTools,
		"user_id":                    req.UserID.String(),
		"message_id":                 enriched.userMessage.ID,
		"tier":                       tier,
		"business_approvals":         enriched.businessApprovals,
		"project_approval_overrides": enriched.projectOverrides,
		"locale":                     req.Locale.String(),
	}
}

// derefString returns the value of p, or "" when nil. The orchestrator
// body builder uses it for *string fields on Business (currently only Website).
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// extractVoiceTone reads the voiceTone tag list out of business.Settings.
// Tags persist as []string under settings.voiceTone (see UpdateVoiceTone).
// JSON round-trips via Postgres come back as []interface{}, so handle both.
// Returns nil when nothing is configured — the orchestrator's system prompt
// treats nil/empty as "no tone preference set". Behavior is byte-identical
// to the previous handler.extractVoiceTone implementation that the legacy
// chat_proxy.go inline builder called.
func extractVoiceTone(settings map[string]interface{}) []string {
	if settings == nil {
		return nil
	}
	raw, ok := settings["voiceTone"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// extractVoiceProfile reads the free-form voiceProfile override out of
// business.Settings. It is a sibling of extractVoiceTone: voiceTone is the tag
// list, voiceProfile is authored prose stored as a single string under
// settings.voiceProfile (see UpdateVoiceProfile). Returns "" when unset — the
// orchestrator renders nothing for an empty profile, so the built prompt stays
// byte-identical to today for every business that never set one.
func extractVoiceProfile(settings map[string]interface{}) string {
	if settings == nil {
		return ""
	}
	raw, ok := settings["voiceProfile"]
	if !ok || raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return s
	}
	return ""
}

// newStreamState constructs an empty streamState with a fresh idMap. Helper
// so Run() doesn't have to know the streamState shape.
func newStreamState() *streamState {
	return &streamState{idMap: make(map[string]string)}
}

// streamStartMessageID returns the message ID Turn.Run reserves for the
// assistant message before opening the stream. Hoisted into a function so
// tests can override (currently identity wrapper around uuid.NewString).
func streamStartMessageID() string {
	return uuid.NewString()
}
