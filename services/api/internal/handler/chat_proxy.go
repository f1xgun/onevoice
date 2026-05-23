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

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/api/internal/handler/chatproxy"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// ResumeBatchHeader is re-exported from chatproxy so callers (router,
// chat_proxy_test.go) keep using `handler.ResumeBatchHeader`.
const ResumeBatchHeader = chatproxy.ResumeBatchHeader

// Package-level lint constants kept here so sibling handlers (titler.go,
// hitl.go) reference one source of truth. Originally introduced in PR #60
// (lint-hardening); preserved across the chatproxy decomposition.
const (
	roleAssistant  = "assistant"
	roleUser       = "user"
	sseBufferBytes = 1 << 20 // 1 MiB — matches chatproxy.OrchestrationProxy
)

// ChatProxyHandler is the thin facade over chatproxy/ collaborators.
// The unexported repo fields stay on the struct
// so chat_proxy_test.go's direct literal `&ChatProxyHandler{messageRepo:
// msgRepo}` continues to compile.
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

// NewChatProxyHandler constructs the facade. Takes the
// shared *orchestratorclient.Client built once in wire.BuildServices instead
// of re-wrapping (orchestratorURL, httpClient) into a fresh client. A nil
// orchClient is auto-built from orchestratorURL+httpClient so existing tests
// can keep passing (orch.URL, nil, ...) without threading the shared client.
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
	orchClient *orchestratorclient.Client,
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
	if orchClient == nil {
		// Tests pass nil to skip the orchestrator handshake; build a no-op
		// client so the field is never nil and the SSE path can return
		// orchestrator-unavailable cleanly.
		orchClient = orchestratorclient.New("", http.DefaultClient)
	}
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
	pauseEvent       *sse.Event
	streamErrContent string
}

// Chat routes the request through the 4-step facade: HITL gate → enrich +
// persist → SSE stream → post-stream persist. HITL flow preserved
// verbatim — see chatproxy/hitl_coordinator.go GateAction* doc.
func (h *ChatProxyHandler) Chat(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "Chat: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentCreate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	conversationID := chi.URLParam(r, "conversationID")

	corrID := logger.CorrelationIDFromContext(r.Context())
	// Capture locale once at the request edge so every persist op + the
	// auto-titler spawn ctx (Phase D2) sees the same language tag the user
	// is chatting in. Resolved from middleware.Locale → i18n.LocaleFromContext.
	reqLocale := i18n.LocaleFromContext(r.Context())
	persistCtx := func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if corrID != "" {
			ctx = logger.WithCorrelationID(ctx, corrID)
		}
		ctx = i18n.WithLocale(ctx, reqLocale)
		return ctx, cancel
	}

	// Step 1: stream-open gate.
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
	enriched, err := h.enricher.Enrich(r.Context(), bc.UserID, conversationID, req)
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

	orchBody, _ := json.Marshal(h.buildOrchRequest(r.Context(), bc.UserID, enriched, req))
	streamErr := h.proxy.StreamChat(r.Context(), w, conversationID, orchBody, nil, func(ev sse.Event) {
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
func (h *ChatProxyHandler) dispatchSSEEvent(taskOpsCtx context.Context, businessID string, state *streamState, idMap map[string]string, ev sse.Event) {
	switch ev.Type {
	case "text":
		state.assistantText.WriteString(ev.Content)
	case "tool_call":
		// anti-footgun #4: propagate the LLM's real tool_call.id.
		state.toolCalls = append(state.toolCalls, domain.ToolCall{
			ID:        ev.ToolCallID,
			Name:      ev.ToolName,
			Arguments: ev.ToolArgs,
		})
		h.postal.OnToolCall(taskOpsCtx, businessID, ev.ToolCallID, ev.ToolName, ev.ToolDisplayName, ev.ToolDisplayNameKey, ev.ToolArgs, idMap)
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
		// copy so the post-loop branch can read BatchID.
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
// (pause branch OR done/error). Auto-title fires on done.
func (h *ChatProxyHandler) persistAfterStream(
	persistCtx func() (context.Context, context.CancelFunc),
	conversationID, streamStartMessageID string,
	enriched *chatproxy.EnrichmentResult,
	req chatproxy.ChatProxyRequest,
	state *streamState,
) {
	// Pause-time persistence.
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
			Role:           roleAssistant,
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
		// Localize at write-time using the locale captured in persistCtx
		// (planted by the request edge from middleware.Locale). The wrapper
		// is persisted to MongoDB so chat history renders in the writer's
		// language forever — we don't retroactively re-translate.
		content = i18n.Tr(saveCtx, "api.chat.stream_error_wrapper", state.streamErrContent)
	}
	assistantMsg := &domain.Message{
		ID:             streamStartMessageID,
		ConversationID: conversationID,
		Role:           roleAssistant,
		Content:        content,
		ToolCalls:      state.toolCalls,
		ToolResults:    state.toolResults,
		Status:         domain.MessageStatusComplete,
	}
	if err := h.persister.PersistAssistantComplete(saveCtx, assistantMsg); err != nil {
		slog.ErrorContext(saveCtx, "failed to save assistant message", "error", err)
	}
	// skip auto-title on errors.
	if state.streamErrContent == "" {
		h.persister.FireAutoTitleIfPending(persistCtx, conversationID, enriched.Business.ID.String(), req.Message, state.assistantText.String())
	}
}

// buildOrchRequest assembles the JSON body forwarded to /chat/{id}. Field
// set is byte-identical to the legacy inline builder so the orchestrator
// continues to receive the exact same shape (no contract change), with the
// single addition of the `locale` field (Phase D1): the resolved per-request
// language tag, sourced from i18n.LocaleFromContext(ctx). The locale is what
// flips the orchestrator's prompt builder between RU and EN templates, which
// in turn flips the LLM's reply language.
func (h *ChatProxyHandler) buildOrchRequest(ctx context.Context, userID uuid.UUID, enriched *chatproxy.EnrichmentResult, req chatproxy.ChatProxyRequest) map[string]interface{} {
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
		// Locale resolved from the API request — the middleware.Locale chain
		// parses Accept-Language (set by the frontend axios interceptor from
		// the NEXT_LOCALE cookie). Emitted as the canonical BCP-47 string
		// ("ru", "en") so the orchestrator's MatchAcceptLanguage on the other
		// side handles both single-tag and preference-list inputs uniformly.
		"locale": i18n.LocaleFromContext(ctx).String(),
	}
}

// === facade wrappers — keep existing chat_proxy_test.go invocations working ===

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
		case roleUser:
			history = append(history, map[string]string{"role": roleUser, "content": m.Content})
		case roleAssistant:
			if m.Content == "" && len(m.ToolCalls) == 0 {
				continue
			}
			history = append(history, map[string]string{"role": roleAssistant, "content": m.Content})
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
