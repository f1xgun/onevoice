package chatturn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// enrichmentResult is the bag of fields the orchestrator request body needs:
// business, active integrations, project fields, history, plus the
// not-yet-persisted user Message that the post-stream step will reference for
// auto-title fanout.
//
// Field names match the orchestrator JSON shape so buildOrchestratorRequest's
// map[string]interface{} stays byte-identical to the legacy chat_proxy.go
// implementation it replaces.
type enrichmentResult struct {
	business           *domain.Business
	activeIntegrations []string
	history            []map[string]string
	userMessage        *domain.Message // ID assigned, NOT yet persisted
	project            projectFields
	businessApprovals  map[string]domain.ToolFloor
	projectOverrides   map[string]domain.ToolFloor
}

// projectFields is the resolved per-project enrichment. Empty fields when the
// conversation has no project (or the lookup failed gracefully — the chat
// flow continues with no project context rather than 500-ing).
type projectFields struct {
	id            string
	name          string
	systemPrompt  string
	whitelistMode string
	allowedTools  []string
}

// enrich resolves business + integrations + project + history and constructs
// the user-message (not yet persisted; persistUserMessage in persist.go writes
// it AFTER enrichment succeeds so a 404 doesn't leave an orphan user msg).
//
// Returns domain.ErrBusinessNotFound unwrapped from the BusinessReader so
// Turn.Run can map it to OutcomeBusinessNotFound without unwrapping again.
// Other repository errors propagate wrapped with a "chatturn:" prefix.
func (t *Turn) enrich(ctx context.Context, req TurnRequest) (*enrichmentResult, error) {
	business, err := t.deps.Business.GetByID(ctx, req.BusinessID)
	if err != nil {
		return nil, err
	}

	integrations, err := t.deps.Integrations.ListByBusinessID(ctx, business.ID)
	if err != nil {
		return nil, fmt.Errorf("chatturn: list integrations: %w", err)
	}
	active := make([]string, 0)
	seen := make(map[string]bool)
	for _, integ := range integrations {
		if integ.Status == "active" && !seen[integ.Platform] {
			active = append(active, integ.Platform)
			seen[integ.Platform] = true
		}
	}

	history := t.loadHistory(ctx, req.ConversationID)

	userMsg := &domain.Message{
		ID:             uuid.NewString(),
		ConversationID: req.ConversationID,
		Role:           "user",
		Content:        req.Message,
	}

	project, projectOverrides := t.resolveProject(ctx, business.ID, req.ConversationID)

	// Normalize nil slices so the outbound JSON serializes as `[]` not `null`
	// (matches the orchestrator's expectation in handler tests).
	if project.allowedTools == nil {
		project.allowedTools = []string{}
	}
	// HITL policy inputs. The defensive Business.ToolApprovals() accessor
	// always returns non-nil; projectOverrides may be nil for the no-project
	// branch — materialize an empty map so the JSON shape stays `{}` not
	// `null` (also expected by the orchestrator).
	businessApprovals := business.ToolApprovals()
	if businessApprovals == nil {
		businessApprovals = map[string]domain.ToolFloor{}
	}
	if projectOverrides == nil {
		projectOverrides = map[string]domain.ToolFloor{}
	}

	return &enrichmentResult{
		business:           business,
		activeIntegrations: active,
		history:            history,
		userMessage:        userMsg,
		project:            project,
		businessApprovals:  businessApprovals,
		projectOverrides:   projectOverrides,
	}, nil
}

// resolveProject looks up the project linked to a conversation (if any) and
// returns its enrichment fields plus its ApprovalOverrides map. Missing
// project / stale ID / lookup failure all fall through to empty fields with a
// warn log — the chat flow continues without project context rather than
// failing the request.
func (t *Turn) resolveProject(ctx context.Context, businessID uuid.UUID, conversationID string) (project projectFields, overrides map[string]domain.ToolFloor) {
	conv, convErr := t.deps.Conversations.GetByID(ctx, conversationID)
	switch {
	case convErr != nil:
		// Missing/errored conversation: log and fall through. Other handlers
		// (GetConversation, move) enforce existence; here we must not break
		// the chat flow.
		slog.WarnContext(ctx, "chatturn: conversation lookup failed, no project enrichment",
			"conversation_id", conversationID, "error", convErr)
	case conv.ProjectID != nil && *conv.ProjectID != "":
		projUUID, parseErr := uuid.Parse(*conv.ProjectID)
		if parseErr != nil {
			slog.WarnContext(ctx, "chatturn: invalid project_id on conversation, falling back to no-project",
				"conversation_id", conversationID, "project_id", *conv.ProjectID, "error", parseErr)
			return project, overrides
		}
		proj, projErr := t.deps.Projects.GetByID(ctx, businessID, projUUID)
		switch {
		case projErr == nil:
			project.id = proj.ID.String()
			project.name = proj.Name
			project.systemPrompt = proj.SystemPrompt
			project.whitelistMode = string(proj.WhitelistMode)
			project.allowedTools = proj.AllowedTools
			overrides = proj.ApprovalOverrides
		case errors.Is(projErr, domain.ErrProjectNotFound):
			slog.WarnContext(ctx, "chatturn: stale project_id, falling back to no-project",
				"conversation_id", conversationID, "project_id", *conv.ProjectID)
		default:
			slog.WarnContext(ctx, "chatturn: failed to resolve project, falling back to no-project",
				"conversation_id", conversationID, "project_id", *conv.ProjectID, "error", projErr)
		}
	}
	return project, overrides
}

// loadHistory fetches prior messages and converts them to the simple
// role/content map the orchestrator's history input expects.
//
// Skips assistant messages with empty content AND no tool_calls — OpenAI /
// OpenRouter 400 on `{role:"assistant", content:""}` between user turns,
// which permanently bricks the conversation. Drop the bad turn from history
// rather than poisoning future requests.
func (t *Turn) loadHistory(ctx context.Context, conversationID string) []map[string]string {
	msgs, err := t.deps.Messages.ListByConversationID(ctx, conversationID, 100, 0)
	if err != nil {
		slog.ErrorContext(ctx, "chatturn: failed to load conversation history", "error", err)
		return nil
	}
	history := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			history = append(history, map[string]string{"role": "user", "content": m.Content})
		case "assistant":
			if m.Content == "" && len(m.ToolCalls) == 0 {
				continue
			}
			history = append(history, map[string]string{"role": "assistant", "content": m.Content})
		}
	}
	return history
}
