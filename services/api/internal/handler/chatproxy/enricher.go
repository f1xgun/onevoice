package chatproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// EnrichmentResult is the bag of fields chat_proxy currently builds inline
// (chat_proxy.go:280-404). Field names match the orchestrator request shape
// so JSON serialization stays byte-identical.
type EnrichmentResult struct {
	Business           *domain.Business
	ActiveIntegrations []string
	History            []map[string]string
	UserMessage        *domain.Message // ID assigned, NOT yet persisted
	Project            ProjectFields
	BusinessApprovals  map[string]domain.ToolFloor
	ProjectOverrides   map[string]domain.ToolFloor
}

// ProjectFields captures the resolved project enrichment. Empty fields when
// the conversation has no project (or the lookup failed gracefully — see
// chat_proxy.go:323-357 for the legacy switch).
type ProjectFields struct {
	ID            string
	Name          string
	SystemPrompt  string
	WhitelistMode string
	AllowedTools  []string
}

// RequestEnricher consolidates business + integration + project + history
// resolution. Errors are returned to the entry handler for HTTP mapping —
// the enricher MUST NOT call writeJSONError directly (19-PATTERNS anti-pattern).
type RequestEnricher struct {
	business     BusinessService
	integrations IntegrationService
	projects     ProjectService
	convs        domain.ConversationRepository
	msgs         domain.MessageRepository
}

// NewRequestEnricher constructs a RequestEnricher. All five dependencies are
// required; nil triggers a panic at construction time (matches chat_proxy.go's
// existing wiring-time invariants for projectService / conversationRepo).
func NewRequestEnricher(
	business BusinessService,
	integrations IntegrationService,
	projects ProjectService,
	convs domain.ConversationRepository,
	msgs domain.MessageRepository,
) *RequestEnricher {
	if business == nil {
		panic("chatproxy.NewRequestEnricher: business cannot be nil")
	}
	if integrations == nil {
		panic("chatproxy.NewRequestEnricher: integrations cannot be nil")
	}
	if projects == nil {
		panic("chatproxy.NewRequestEnricher: projects cannot be nil")
	}
	if convs == nil {
		panic("chatproxy.NewRequestEnricher: convs cannot be nil")
	}
	if msgs == nil {
		panic("chatproxy.NewRequestEnricher: msgs cannot be nil")
	}
	return &RequestEnricher{
		business:     business,
		integrations: integrations,
		projects:     projects,
		convs:        convs,
		msgs:         msgs,
	}
}

// Enrich resolves business, active_integrations, project_*, history, and
// builds the not-yet-persisted user Message. Errors map to the entry
// handler's existing sentinel-based HTTP responses (ErrBusinessNotFound→404).
func (e *RequestEnricher) Enrich(ctx context.Context, userID uuid.UUID, conversationID string, body ChatProxyRequest) (*EnrichmentResult, error) {
	business, err := e.business.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	integrations, err := e.integrations.ListByBusinessID(ctx, business.ID)
	if err != nil {
		return nil, fmt.Errorf("chatproxy: list integrations: %w", err)
	}
	activeIntegrations := make([]string, 0)
	seen := make(map[string]bool)
	for _, integ := range integrations {
		if integ.Status == "active" && !seen[integ.Platform] {
			activeIntegrations = append(activeIntegrations, integ.Platform)
			seen[integ.Platform] = true
		}
	}

	history := e.loadHistoryInternal(ctx, conversationID)

	userMsg := &domain.Message{
		ID:             uuid.NewString(),
		ConversationID: conversationID,
		Role:           "user",
		Content:        body.Message,
	}

	project := ProjectFields{}
	var projectOverrides map[string]domain.ToolFloor

	conv, convErr := e.convs.GetByID(ctx, conversationID)
	switch {
	case convErr != nil:
		// Missing/errored conversation: log and fall through to no-project
		// enrichment. Other handlers (GetConversation, move) enforce
		// existence; here we must not break the chat flow.
		slog.WarnContext(ctx, "chat proxy: conversation lookup failed, no project enrichment",
			"conversation_id", conversationID, "error", convErr)
	case conv.ProjectID != nil && *conv.ProjectID != "":
		projUUID, parseErr := uuid.Parse(*conv.ProjectID)
		if parseErr != nil {
			slog.WarnContext(ctx, "chat proxy: invalid project_id on conversation, falling back to no-project",
				"conversation_id", conversationID, "project_id", *conv.ProjectID, "error", parseErr)
		} else {
			proj, projErr := e.projects.GetByID(ctx, business.ID, projUUID)
			switch {
			case projErr == nil:
				project.ID = proj.ID.String()
				project.Name = proj.Name
				project.SystemPrompt = proj.SystemPrompt
				project.WhitelistMode = string(proj.WhitelistMode)
				project.AllowedTools = proj.AllowedTools
				// Plan 17-07 GAP-03: capture per-project ToolFloor overrides
				// (POLICY-03) so hitl.Resolve at pause time has the project
				// inputs alongside business_approvals (POLICY-02).
				projectOverrides = proj.ApprovalOverrides
			case errors.Is(projErr, domain.ErrProjectNotFound):
				slog.WarnContext(ctx, "chat proxy: stale project_id, falling back to no-project",
					"conversation_id", conversationID, "project_id", *conv.ProjectID)
			default:
				slog.WarnContext(ctx, "chat proxy: failed to resolve project, falling back to no-project",
					"conversation_id", conversationID, "project_id", *conv.ProjectID, "error", projErr)
			}
		}
	}
	// Normalize nil slices so the outbound JSON serializes as `[]` not `null`
	// (matches the orchestrator's expectation from Plan 15-02 handler tests).
	if project.AllowedTools == nil {
		project.AllowedTools = []string{}
	}

	// Plan 17-07 GAP-03: HITL policy inputs. The defensive accessor
	// Business.ToolApprovals() always returns non-nil; project.ApprovalOverrides
	// may be nil (no project or stale ID), so we materialize an empty map to
	// keep the JSON shape `{}` not `null`.
	businessApprovals := business.ToolApprovals()
	if businessApprovals == nil {
		businessApprovals = map[string]domain.ToolFloor{}
	}
	if projectOverrides == nil {
		projectOverrides = map[string]domain.ToolFloor{}
	}

	return &EnrichmentResult{
		Business:           business,
		ActiveIntegrations: activeIntegrations,
		History:            history,
		UserMessage:        userMsg,
		Project:            project,
		BusinessApprovals:  businessApprovals,
		ProjectOverrides:   projectOverrides,
	}, nil
}

// LoadHistory exposes the same projection the entry handler facade keeps for
// chat_proxy_test.go compatibility. Implementation is shared with Enrich via
// loadHistoryInternal.
func (e *RequestEnricher) LoadHistory(ctx context.Context, conversationID string) []map[string]string {
	return e.loadHistoryInternal(ctx, conversationID)
}

// loadHistoryInternal fetches prior messages and converts them to the simple
// role/content map format expected by the orchestrator.
//
// Skips assistant messages with empty content AND no tool_calls — OpenAI/
// OpenRouter 400 on `{role:"assistant", content:""}` between user turns,
// which permanently bricks the conversation. Drop the bad turn from history.
func (e *RequestEnricher) loadHistoryInternal(ctx context.Context, conversationID string) []map[string]string {
	msgs, err := e.msgs.ListByConversationID(ctx, conversationID, 100, 0)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load conversation history", "error", err)
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
