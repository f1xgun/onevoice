package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/metrics"
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

// defaultHistoryLimit is the fallback chat-history fetch limit when a handler
// is constructed without a configured limit (struct-literal test path). The
// operator-facing knob lives in config (MESSAGE_HISTORY_LIMIT).
const defaultHistoryLimit = 100

// ChatProxyHandler is a thin HTTP-facade over chatturn.Turn — parses, gates,
// maps TurnOutcome to HTTP. See docs/api/handlers/chat-proxy.md.
//
// messageRepo is retained for one legacy test (TestChatProxy_LoadHistory_SkipsEmptyAssistant)
// that constructs the handler via struct literal; loadHistory reads it
// directly so the test does not need a fully-wired *chatturn.Turn.
type ChatProxyHandler struct {
	turn        *chatturn.Turn
	messageRepo domain.MessageRepository

	// historyLimit caps how many prior messages loadHistory fetches. Mirrors
	// the value threaded into chatturn.Deps so the legacy struct-literal test
	// path stays consistent with the production lifecycle.
	historyLimit int

	// sseCounter caps in-flight SSE streams per user; nil disables the gate.
	sseCounter *ssecounter.Counter

	// defaultTier labels the SSE concurrency block metric; empty → "free".
	defaultTier string
}

// Turn exposes the shared chat-turn lifecycle so sibling handlers (HITLHandler.Resume)
// can delegate to the same persisting resume path instead of building a second Turn.
func (h *ChatProxyHandler) Turn() *chatturn.Turn { return h.turn }

// SetSSECounter wires the per-user SSE concurrency cap (optional).
func (h *ChatProxyHandler) SetSSECounter(c *ssecounter.Counter, defaultTier string) {
	h.sseCounter = c
	if defaultTier == "" {
		defaultTier = defaultSSETier
	}
	h.defaultTier = defaultTier
}

// retryAfterSeconds is advertised in the HTTP 429 Retry-After header; 1s is
// honest because the counter releases on any same-user stream completion.
const retryAfterSeconds = 1

// defaultSSETier is the fallback label for the SSE concurrency block metric
// when no tier is configured. Shared by every SSE-streaming handler.
const defaultSSETier = "free"

// writeConcurrencyError maps SSE counter sentinels to the JSON wire shape.
// Package-level so every SSE-streaming handler (ChatProxyHandler.Chat,
// HITLHandler.Resume) maps the same Acquire failures to identical HTTP
// responses. See docs/api/handlers/chat-proxy.md §"Per-user SSE concurrency cap".
func writeConcurrencyError(w http.ResponseWriter, err error) {
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

// NewChatProxyHandler builds the chatturn.Turn from its dependency set. See
// docs/api/handlers/chat-proxy.md §"Construction".
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
	auditLogger audit.Logger,
	historyLimit int,
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
		Audit:         auditLogger,
		HistoryLimit:  historyLimit,
	})

	return &ChatProxyHandler{
		turn:         turn,
		messageRepo:  messageRepo,
		historyLimit: historyLimit,
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
	bc, ok := requireBusiness(w, r, "Chat", authz.PermContentCreate)
	if !ok {
		return
	}

	if h.sseCounter != nil {
		tier := h.defaultTier
		if tier == "" {
			tier = defaultSSETier
		}
		release, acqErr := h.sseCounter.Acquire(r.Context(), bc.UserID, tier)
		if acqErr != nil {
			writeConcurrencyError(w, acqErr)
			return
		}
		defer release()
	}

	conversationID := chi.URLParam(r, "conversationID")

	headerBatch := r.Header.Get(ResumeBatchHeader)
	if headerBatch == "" {
		headerBatch = r.URL.Query().Get("batch_id")
	}

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
		Locale:         resolveTurnLocale(r.Context(), body.Locale),
	}

	outcome, err := h.turn.Run(r.Context(), w, req, nil)
	metrics.IncChatTurn(outcome.String())
	switch outcome {
	case chatturn.OutcomeMissingMessage:
		writeJSONError(w, http.StatusBadRequest, "message is required")
	case chatturn.OutcomeBusinessNotFound:
		writeJSONError(w, http.StatusNotFound, "business not found")
	case chatturn.OutcomeConversationNotFound:
		writeJSONError(w, http.StatusNotFound, "conversation not found")
	case chatturn.OutcomeOrchestratorUnavailable:
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
	case chatturn.OutcomeError:
		if err != nil {
			slog.ErrorContext(r.Context(), "chat turn errored", "error", err)
		}
	default:
	}
}

// resolveTurnLocale picks the LLM response language: the request-body locale
// wins over the Accept-Language-derived context locale. The body is the only
// channel the browser can use — fetch forbids setting Accept-Language — so
// without this the LLM language follows the browser's Accept-Language instead
// of the user's OneVoice UI locale. Mirrors the orchestrator's own body-wins
// precedence (resolveChatLocale).
func resolveTurnLocale(ctx context.Context, bodyLocale *string) language.Tag {
	if bodyLocale != nil && *bodyLocale != "" {
		return i18n.MatchAcceptLanguage(*bodyLocale)
	}
	return i18n.LocaleFromContext(ctx)
}

// loadHistory loads conversation history through the message repository.
func (h *ChatProxyHandler) loadHistory(ctx context.Context, conversationID string) []map[string]string {
	limit := h.historyLimit
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	msgs, err := h.messageRepo.ListByConversationID(ctx, conversationID, limit, 0)
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
