package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/chatturn"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// ResumeBatchHeader is the explicit HITL resume header. Stays in the handler
// package so the router and existing tests keep using handler.ResumeBatchHeader.
const ResumeBatchHeader = "X-Onevoice-Resume-Batch-Id"

// Package-level constants kept on the handler so sibling handlers
// (titler.go, hitl.go) reference one source of truth. sseBufferBytes is the
// scanner buffer cap used by the resume-stream path in hitl.go; matches
// chatturn's stream scanner so large tool results flow through both paths
// without truncation.
const (
	roleAssistant  = "assistant"
	roleUser       = "user"
	sseBufferBytes = 1 << 20 // 1 MiB
)

// ChatProxyHandler is the thin HTTP-facade over services/api/internal/service/chatturn.Turn.
//
// Responsibilities:
//   - request parsing (body decode, header / URL params)
//   - auth gating (BusinessContext, PermContentCreate)
//   - TurnOutcome → HTTP-status mapping when no SSE bytes have flowed yet
//
// Everything else (gate / enrich / stream / persist / postal) lives in
// services/api/internal/service/chatturn. See CONTEXT.md §"Chat turn".
//
// The messageRepo field is retained for one legacy test that constructs the
// handler with a struct literal (TestChatProxy_LoadHistory_SkipsEmptyAssistant);
// loadHistory below reads it directly so that test does not depend on a
// fully wired *chatturn.Turn.
type ChatProxyHandler struct {
	turn        *chatturn.Turn
	messageRepo domain.MessageRepository
}

// NewChatProxyHandler keeps the legacy 12-arg signature so the wire package
// and existing tests continue to compile unchanged. Internally constructs
// the chatturn.Turn that owns the lifecycle.
//
// A nil orchClient is replaced with a no-op client built from an empty URL —
// preserves the chat_proxy_test.go pattern where tests pass nil to skip the
// orchestrator handshake.
//
// A nil titler short-circuits to chatturn.Deps.Titler = nil so the auto-
// title gate in chatturn returns early without doing a needless conversation
// re-read.
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
	var titlerImpl chatturn.Titler
	if titler != nil {
		titlerImpl = titlerAdapter{titler: titler}
	}

	turn := chatturn.New(chatturn.Deps{
		Business:      businessService,
		Integrations:  integrationService,
		Projects:      projectService,
		Conversations: conversationRepo,
		Messages:      messageRepo,
		Pending:       pendingRepo,
		Posts:         postRepo,
		Reviews:       reviewRepo,
		AgentTasks:    agentTaskRepo,
		TaskHub:       taskHub,
		Orch:          orchClient,
		Titler:        titlerImpl,
	})

	return &ChatProxyHandler{
		turn:        turn,
		messageRepo: messageRepo,
	}
}

// titlerAdapter satisfies chatturn.Titler around the concrete *service.Titler.
// Kept as a small struct so chatturn doesn't have to know about
// service.Titler's wider surface.
type titlerAdapter struct{ titler *service.Titler }

func (a titlerAdapter) GenerateAndSave(ctx context.Context, businessID, conversationID, userText, assistantText string) {
	a.titler.GenerateAndSave(ctx, businessID, conversationID, userText, assistantText)
}

// chatProxyRequest is the JSON body shape; an unexported handler-local type
// so the wire contract stays under the handler's control.
type chatProxyRequest struct {
	Model   string `json:"model"`
	Message string `json:"message"`
}

// Chat parses the request, delegates the lifecycle to Turn.Run, and maps
// the returned TurnOutcome to an HTTP status code when no SSE bytes have
// been written yet.
//
// Once any SSE byte hits the wire (gate non-Fresh branches, or the fresh
// stream successfully forwarding orchestrator output), the response body
// is committed and HTTP-status mapping is moot — those outcomes fall
// through silently.
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

	headerBatch := r.Header.Get(ResumeBatchHeader)
	if headerBatch == "" {
		headerBatch = r.URL.Query().Get("batch_id")
	}

	// Body decode is skipped for explicit-resume calls — they reuse the
	// already-persisted user message and the request body is empty. For
	// fresh calls we decode unconditionally; the Message-required check is
	// enforced INSIDE Turn.Run's Fresh branch (via OutcomeMissingMessage)
	// so the gate can still route an empty-body request to inline-error /
	// re-emit-approval before any body validation fires.
	var body chatProxyRequest
	if headerBatch == "" {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	req := chatturn.TurnRequest{
		BusinessID:     bc.BusinessID,
		UserID:         bc.UserID,
		ConversationID: conversationID,
		Message:        body.Message,
		Model:          body.Model,
		ResumeBatchID:  headerBatch,
		Locale:         i18n.LocaleFromContext(r.Context()),
	}

	outcome, err := h.turn.Run(r.Context(), w, req, nil)
	switch outcome {
	case chatturn.OutcomeMissingMessage:
		writeJSONError(w, http.StatusBadRequest, "message is required")
	case chatturn.OutcomeBusinessNotFound:
		writeJSONError(w, http.StatusNotFound, "business not found")
	case chatturn.OutcomeOrchestratorUnavailable:
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
	case chatturn.OutcomeError:
		// Bytes may already be on the wire; only log.
		if err != nil {
			slog.ErrorContext(r.Context(), "chat turn errored", "error", err)
		}
	default:
		// OutcomeDone / OutcomePauseHITL / OutcomeRejoinedResume /
		// OutcomeReemittedApproval / OutcomeInlineError — SSE bytes already
		// committed, nothing left for the handler to do.
	}
}

// loadHistory is a compat shim used by TestChatProxy_LoadHistory_SkipsEmptyAssistant,
// which constructs &ChatProxyHandler{messageRepo: msgRepo} directly without
// wiring Turn. Reads h.messageRepo so the test stays valid.
//
// Production callers should not use this — Turn.Run loads history through
// chatturn's own loadHistory (which uses the same projection rules).
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

// fireAutoTitleIfPending / fireAutoTitleIfPendingResume are test-only
// wrappers that adapt the legacy persistCtx closure to chatturn.Turn's
// ctx-based public API. The closure pattern was unique to the legacy
// handler shape; preserved here so the existing chat_proxy_test.go suites
// pass unchanged.
func (h *ChatProxyHandler) fireAutoTitleIfPending(persistCtx func() (context.Context, context.CancelFunc), conversationID, businessID, userText, assistantText string) {
	parentCtx, cancel := persistCtx()
	defer cancel()
	h.turn.FireAutoTitleIfPending(parentCtx, conversationID, businessID, userText, assistantText)
}

func (h *ChatProxyHandler) fireAutoTitleIfPendingResume(persistCtx func() (context.Context, context.CancelFunc), conversationID string, assistantMsg *domain.Message) {
	parentCtx, cancel := persistCtx()
	defer cancel()
	h.turn.FireAutoTitleIfPendingResume(parentCtx, conversationID, assistantMsg)
}
