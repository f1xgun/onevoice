package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/services/api/internal/handler/chatproxy"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// ResumeBatchHeader is re-exported from chatproxy so callers (router,
// chat_proxy_test.go) keep using `handler.ResumeBatchHeader`.
const ResumeBatchHeader = chatproxy.ResumeBatchHeader

// ChatProxyHandler is the thin facade over chatproxy/ collaborators
// (Phase 19 / D-03 / D-06). The unexported repo fields stay on the struct
// so chat_proxy_test.go's direct literal `&ChatProxyHandler{messageRepo:
// msgRepo}` continues to compile (D-16).
type ChatProxyHandler struct {
	businessService    BusinessService
	integrationService IntegrationService
	projectService     ProjectService
	conversationRepo   domain.ConversationRepository
	messageRepo        domain.MessageRepository
	pendingRepo        domain.PendingToolCallRepository
	postRepo           domain.PostRepository
	reviewRepo         domain.ReviewRepository
	agentTaskRepo      domain.AgentTaskRepository
	taskHub            *taskhub.Hub
	orchClient         *orchestratorclient.Client
	titler             *service.Titler

	enricher  *chatproxy.RequestEnricher
	proxy     *chatproxy.OrchestrationProxy
	persister *chatproxy.MessagePersister
	postal    *chatproxy.PostalService
	hitl      *chatproxy.HITLCoordinator
}

// NewChatProxyHandler constructs the facade. Signature preserved from
// Phase 18 so wire/handlers.go and existing tests compile unchanged. The
// orchestratorURL+httpClient pair is wrapped in *orchestratorclient.Client
// (D-11) and shared with all collaborators.
func NewChatProxyHandler(
	businessService BusinessService,
	integrationService IntegrationService,
	projectService ProjectService,
	conversationRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
	pendingRepo domain.PendingToolCallRepository,
	postRepo domain.PostRepository,
	reviewRepo domain.ReviewRepository,
	agentTaskRepo domain.AgentTaskRepository,
	taskHub *taskhub.Hub,
	orchestratorURL string,
	httpClient *http.Client,
	titler *service.Titler,
) *ChatProxyHandler {
	if projectService == nil {
		panic("NewChatProxyHandler: projectService cannot be nil")
	}
	if conversationRepo == nil {
		panic("NewChatProxyHandler: conversationRepo cannot be nil")
	}
	if pendingRepo == nil {
		panic("NewChatProxyHandler: pendingRepo cannot be nil")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	orchClient := orchestratorclient.New(orchestratorURL, httpClient)
	persister := chatproxy.NewMessagePersister(messageRepo, conversationRepo, titlerAdapter{titler})
	proxy := chatproxy.NewOrchestrationProxy(orchClient)

	return &ChatProxyHandler{
		businessService:    businessService,
		integrationService: integrationService,
		projectService:     projectService,
		conversationRepo:   conversationRepo,
		messageRepo:        messageRepo,
		pendingRepo:        pendingRepo,
		postRepo:           postRepo,
		reviewRepo:         reviewRepo,
		agentTaskRepo:      agentTaskRepo,
		taskHub:            taskHub,
		orchClient:         orchClient,
		titler:             titler,
		enricher:           chatproxy.NewRequestEnricher(businessService, integrationService, projectService, conversationRepo, messageRepo),
		proxy:              proxy,
		persister:          persister,
		postal:             chatproxy.NewPostalService(postRepo, reviewRepo, agentTaskRepo, taskHub),
		hitl:               chatproxy.NewHITLCoordinator(pendingRepo, messageRepo, persister, orchClient),
	}
}

// titlerAdapter satisfies chatproxy.TitlerService while accepting a nil
// concrete *service.Titler (graceful disable). The wrapped pointer is
// nil-checked on every call so the interface dynamic type doesn't bypass
// the guard.
type titlerAdapter struct{ titler *service.Titler }

func (a titlerAdapter) GenerateAndSave(ctx context.Context, businessID, conversationID, userText, assistantText string) {
	if a.titler == nil {
		return
	}
	a.titler.GenerateAndSave(ctx, businessID, conversationID, userText, assistantText)
}

// streamState is per-request state collected by the SSE event handler;
// passed by pointer so the closure can mutate it.
type streamState struct {
	assistantText    strings.Builder
	toolCalls        []domain.ToolCall
	toolResults      []domain.ToolResult
	pauseEvent       *chatproxy.SSEPayload
	streamErrContent string
}

// Chat routes the request through the 4-step facade: HITL gate → enrich +
// persist → SSE stream → post-stream persist. Phase 16 HITL flow preserved
// verbatim — see chatproxy/hitl_coordinator.go GateAction* doc.
func (h *ChatProxyHandler) Chat(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	conversationID := chi.URLParam(r, "conversationID")

	corrID := logger.CorrelationIDFromContext(r.Context())
	persistCtx := func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if corrID != "" {
			ctx = logger.WithCorrelationID(ctx, corrID)
		}
		return ctx, cancel
	}

	// Step 1: D-04 stream-open gate.
	headerBatch := r.Header.Get(ResumeBatchHeader)
	if headerBatch == "" {
		headerBatch = r.URL.Query().Get("batch_id")
	}
	action, activeMsg, batch, batchID, gateErr := h.hitl.GateOnRequest(r.Context(), conversationID, headerBatch)
	if gateErr != nil {
		slog.ErrorContext(r.Context(), "chat proxy: HITL gate failed", "error", gateErr)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	switch action {
	case chatproxy.GateActionFresh:
		// Fall through to Step 2 (no active message + no resume header).
	case chatproxy.GateActionRejoinResume:
		h.hitl.StreamResume(w, r, conversationID, activeMsg, batchID, persistCtx)
		return
	case chatproxy.GateActionReemitApproval:
		h.hitl.ReemitApprovalEvent(w, batch)
		return
	case chatproxy.GateActionInlineError:
		h.hitl.SSEInlineError(w, "turn_already_in_progress")
		return
	}

	// Step 2: parse body, enrich, persist user message.
	var req chatproxy.ChatProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeJSONError(w, http.StatusBadRequest, "message is required")
		return
	}
	enriched, err := h.enricher.Enrich(r.Context(), userID, conversationID, req)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "chat proxy: enrich failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.persister.PersistUserMessage(r.Context(), enriched.UserMessage); err != nil {
		slog.ErrorContext(r.Context(), "failed to save user message", "error", err)
	}

	// Step 3: open orchestrator stream, dispatch SSE events.
	streamStartMessageID := uuid.NewString()
	state := &streamState{}
	idMap := make(map[string]string)

	taskOpsCtx, cancelTaskOps := context.WithTimeout(context.Background(), 10*time.Minute)
	if corrID != "" {
		taskOpsCtx = logger.WithCorrelationID(taskOpsCtx, corrID)
	}
	defer cancelTaskOps()

	orchBody, _ := json.Marshal(h.buildOrchRequest(userID, enriched, req))
	streamErr := h.proxy.StreamChat(r.Context(), w, conversationID, orchBody, nil, func(ev chatproxy.SSEPayload) {
		h.dispatchSSEEvent(taskOpsCtx, enriched.Business.ID.String(), state, idMap, ev)
	})
	if streamErr != nil && state.pauseEvent == nil && state.streamErrContent == "" &&
		!errors.Is(streamErr, context.Canceled) && strings.Contains(streamErr.Error(), "stream chat") {
		// Surface 502 only when we never received any SSE bytes (dial refused).
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
		return
	}

	// Step 4: post-stream persistence + side effects.
	h.persistAfterStream(persistCtx, conversationID, streamStartMessageID, enriched, req, state)
	if h.postRepo != nil || h.reviewRepo != nil {
		sideCtx, cancel := persistCtx()
		defer cancel()
		h.postal.RecordPostsAndReviews(sideCtx, enriched.Business.ID.String(), state.toolCalls, state.toolResults)
	}
}

// dispatchSSEEvent routes a single parsed SSE frame into the per-stream state
// + collaborator side effects. Per-stream state is owned by the caller.
func (h *ChatProxyHandler) dispatchSSEEvent(taskOpsCtx context.Context, businessID string, state *streamState, idMap map[string]string, ev chatproxy.SSEPayload) {
	switch ev.Type {
	case "text":
		state.assistantText.WriteString(ev.Content)
	case "tool_call":
		// HITL-13 / anti-footgun #4: propagate the LLM's real tool_call.id.
		state.toolCalls = append(state.toolCalls, domain.ToolCall{
			ID:        ev.ToolCallID,
			Name:      ev.ToolName,
			Arguments: ev.ToolArgs,
		})
		h.postal.OnToolCall(taskOpsCtx, businessID, ev.ToolCallID, ev.ToolName, ev.ToolDisplayName, ev.ToolArgs, idMap)
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
		})
		h.postal.OnToolResult(taskOpsCtx, businessID, ev.ToolCallID, content, ev.ToolError, idMap)
	case "tool_approval_required":
		// HITL-01 / HITL-02: copy so the post-loop branch can read BatchID.
		evCopy := ev
		state.pauseEvent = &evCopy
	case "error":
		// Captured for assistant Message persist (avoids empty content
		// poisoning loadHistory on the next turn).
		state.streamErrContent = ev.Content
	}
	// "tool_rejected": forward-only — paired Message persisted in pause/done path.
}

// persistAfterStream writes the assistant Message after the SSE loop drains
// (pause-time HITL-01 branch OR done/error). Auto-title fires on done.
func (h *ChatProxyHandler) persistAfterStream(
	persistCtx func() (context.Context, context.CancelFunc),
	conversationID, streamStartMessageID string,
	enriched *chatproxy.EnrichmentResult,
	req chatproxy.ChatProxyRequest,
	state *streamState,
) {
	// Pause-time persistence (Phase 16 HITL-01).
	if state.pauseEvent != nil {
		saveCtx, cancel := persistCtx()
		defer cancel()
		pendingToolCalls := make([]domain.ToolCall, 0, len(state.toolCalls))
		for _, tc := range state.toolCalls {
			pendingToolCalls = append(pendingToolCalls, domain.ToolCall{
				ID:         tc.ID,
				Name:       tc.Name,
				Arguments:  tc.Arguments,
				ApprovalID: fmt.Sprintf("%s-%s", state.pauseEvent.BatchID, tc.ID),
				Status:     domain.ToolCallStatusPending,
			})
		}
		assistantMsg := &domain.Message{
			ID:             streamStartMessageID,
			ConversationID: conversationID,
			Role:           "assistant",
			Content:        state.assistantText.String(),
			ToolCalls:      pendingToolCalls,
			Status:         domain.MessageStatusPendingApproval,
		}
		if err := h.persister.PersistAssistantPause(saveCtx, assistantMsg); err != nil {
			slog.WarnContext(saveCtx, "failed to persist assistant pending_approval message",
				"error", err, "conversation_id", conversationID, "batch_id", state.pauseEvent.BatchID)
		}
		return
	}

	// Done / error persistence.
	if state.assistantText.Len() == 0 && len(state.toolCalls) == 0 && state.streamErrContent == "" {
		return
	}
	saveCtx, cancel := persistCtx()
	defer cancel()
	content := state.assistantText.String()
	if content == "" && state.streamErrContent != "" {
		content = "[Ошибка: " + state.streamErrContent + "]"
	}
	assistantMsg := &domain.Message{
		ID:             streamStartMessageID,
		ConversationID: conversationID,
		Role:           "assistant",
		Content:        content,
		ToolCalls:      state.toolCalls,
		ToolResults:    state.toolResults,
		Status:         domain.MessageStatusComplete,
	}
	if err := h.persister.PersistAssistantComplete(saveCtx, assistantMsg); err != nil {
		slog.ErrorContext(saveCtx, "failed to save assistant message", "error", err)
	}
	// Phase 18 / TITLE-02 / D-01: skip auto-title on errors.
	if state.streamErrContent == "" {
		h.persister.FireAutoTitleIfPending(persistCtx, conversationID, enriched.Business.ID.String(), req.Message, state.assistantText.String())
	}
}

// buildOrchRequest assembles the JSON body forwarded to /chat/{id}. Field
// set is byte-identical to the legacy inline builder so the orchestrator
// continues to receive the exact same shape (no contract change).
func (h *ChatProxyHandler) buildOrchRequest(userID uuid.UUID, enriched *chatproxy.EnrichmentResult, req chatproxy.ChatProxyRequest) map[string]interface{} {
	business := enriched.Business
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
		"active_integrations":        enriched.ActiveIntegrations,
		"history":                    enriched.History,
		"project_id":                 enriched.Project.ID,
		"project_name":               enriched.Project.Name,
		"project_system_prompt":      enriched.Project.SystemPrompt,
		"project_whitelist_mode":     enriched.Project.WhitelistMode,
		"project_allowed_tools":      enriched.Project.AllowedTools,
		"user_id":                    userID.String(),
		"message_id":                 enriched.UserMessage.ID,
		"tier":                       "",
		"business_approvals":         enriched.BusinessApprovals,
		"project_approval_overrides": enriched.ProjectOverrides,
	}
}

// === D-16 facade wrappers — keep existing chat_proxy_test.go invocations working ===

// loadHistory keeps the same projection chat_proxy_test.go:261 asserts.
// Implementation is duplicated (not delegated to enricher) so the test at
// line 260 — which constructs `&ChatProxyHandler{messageRepo: msgRepo}`
// directly without an Enricher — continues to work unchanged.
func (h *ChatProxyHandler) loadHistory(ctx context.Context, conversationID string) []map[string]string {
	msgs, err := h.messageRepo.ListByConversationID(ctx, conversationID, 100, 0)
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

func (h *ChatProxyHandler) fireAutoTitleIfPending(persistCtx func() (context.Context, context.CancelFunc), conversationID, businessID, userText, assistantText string) {
	h.persister.FireAutoTitleIfPending(persistCtx, conversationID, businessID, userText, assistantText)
}

func (h *ChatProxyHandler) fireAutoTitleIfPendingResume(persistCtx func() (context.Context, context.CancelFunc), conversationID string, assistantMsg *domain.Message) {
	h.persister.FireAutoTitleIfPendingResume(persistCtx, conversationID, assistantMsg)
}
