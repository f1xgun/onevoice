package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/ratelimit"
	"github.com/f1xgun/onevoice/pkg/ssecounter"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/chatturn"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// ResumeBatchHeader is the explicit HITL resume header. Pinned to the handler
// package so router + tests can keep using handler.ResumeBatchHeader.
const ResumeBatchHeader = "X-Onevoice-Resume-Batch-Id"

// ChatProxyHandler is a thin HTTP-facade over chatturn.Turn — parses, gates,
// maps TurnOutcome to HTTP. See docs/api/handlers/chat-proxy.md.
//
// messageRepo is retained for one legacy test (TestChatProxy_LoadHistory_SkipsEmptyAssistant)
// that constructs the handler via struct literal; loadHistory reads it
// directly so the test does not need a fully-wired *chatturn.Turn.
type ChatProxyHandler struct {
	turn        *chatturn.Turn
	messageRepo domain.MessageRepository

	// sseCounter caps in-flight SSE streams per user; nil disables the gate.
	sseCounter *ssecounter.Counter

	// defaultTier labels the SSE concurrency block metric; empty → "free".
	defaultTier string
}

// SetSSECounter wires the per-user SSE concurrency cap (optional).
func (h *ChatProxyHandler) SetSSECounter(c *ssecounter.Counter, defaultTier string) {
	h.sseCounter = c
	if defaultTier == "" {
		defaultTier = "free"
	}
	h.defaultTier = defaultTier
}

// retryAfterSeconds is advertised in the HTTP 429 Retry-After header; 1s is
// honest because the counter releases on any same-user stream completion.
const retryAfterSeconds = 1

// writeConcurrencyError maps SSE counter sentinels to the JSON wire shape.
// See docs/api/handlers/chat-proxy.md §"Per-user SSE concurrency cap".
func (h *ChatProxyHandler) writeConcurrencyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ssecounter.ErrConcurrencyExceeded):
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSeconds))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":          "sse_concurrency_exceeded",
			"retry_after_s": retryAfterSeconds,
		})
	case errors.Is(err, ratelimit.ErrUnavailable), errors.Is(err, ratelimit.ErrExceeded):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "rate_limit_unavailable",
		})
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "rate_limit_unavailable",
		})
	}
}

// NewChatProxyHandler keeps the legacy 12-arg signature; internally builds
// the chatturn.Turn. See docs/api/handlers/chat-proxy.md §"Construction".
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
		// Tests pass nil; no-op client keeps the SSE path's unavailable branch clean.
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

// titlerAdapter narrows *service.Titler down to chatturn.Titler so chatturn
// doesn't have to import the wider service.Titler surface.
type titlerAdapter struct{ titler *service.Titler }

func (a titlerAdapter) GenerateAndSave(ctx context.Context, businessID, conversationID, userText, assistantText string) {
	a.titler.GenerateAndSave(ctx, businessID, conversationID, userText, assistantText)
}

// Chat handles POST /chat/{conversationID}; delegates lifecycle to Turn.Run
// and maps TurnOutcome → HTTP. See docs/api/handlers/chat-proxy.md.
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

	// Acquire BEFORE any SSE header write so a rejection is a JSON 429,
	// never a half-stream. release() is idempotent.
	if h.sseCounter != nil {
		tier := h.defaultTier
		if tier == "" {
			tier = "free"
		}
		release, acqErr := h.sseCounter.Acquire(r.Context(), bc.UserID, tier)
		if acqErr != nil {
			h.writeConcurrencyError(w, acqErr)
			return
		}
		defer release()
	}

	conversationID := chi.URLParam(r, "conversationID")

	headerBatch := r.Header.Get(ResumeBatchHeader)
	if headerBatch == "" {
		headerBatch = r.URL.Query().Get("batch_id")
	}

	// Explicit-resume calls reuse the persisted user message → skip decode.
	// Fresh calls decode unconditionally so an empty Message still reaches
	// Turn.Run's gate (inline-error / re-emit-approval branches).
	var body openapi.ChatTurnRequest
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
		Message:        strDeref(body.Message),
		Model:          strDeref(body.Model),
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
		// Bytes may already be on the wire — log only, do not overwrite.
		if err != nil {
			slog.ErrorContext(r.Context(), "chat turn errored", "error", err)
		}
	default:
		// SSE bytes already committed for OutcomeDone / PauseHITL /
		// RejoinedResume / ReemittedApproval / InlineError — nothing to do.
	}
}

// loadHistory loads conversation history through the message repository.
func (h *ChatProxyHandler) loadHistory(ctx context.Context, conversationID string) []map[string]string {
	msgs, err := h.messageRepo.ListByConversationID(ctx, conversationID, 100, 0)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load conversation history", "error", err)
		return nil
	}
	history := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case domain.MessageRoleUser:
			history = append(history, map[string]string{"role": domain.MessageRoleUser, "content": m.Content})
		case domain.MessageRoleAssistant:
			if m.Content == "" && len(m.ToolCalls) == 0 {
				continue
			}
			history = append(history, map[string]string{"role": domain.MessageRoleAssistant, "content": m.Content})
		}
	}
	return history
}

// fireAutoTitleIfPending / fireAutoTitleIfPendingResume are test-only
// adapters around chatturn.Turn's ctx-based public API. NOT for production.
// See docs/api/handlers/chat-proxy.md §"Test-only entry points".
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
