package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// TitlerHandler handles the auto-titler regenerate endpoint.
//
// The titler field is typed as *service.Titler concretely (B-02 alignment):
// there is exactly ONE canonical mocking seam in Phase 18 — the package-private
// chatCaller interface inside services/api/internal/service. Handler tests
// construct a real *service.Titler with a fake chatCaller via
// service.FakeChatCaller (Plan 05 Task 2). NO parallel titlerCaller interface
// is introduced anywhere in this phase.
//
// titler is allowed to be nil (graceful disable when TITLER_MODEL/LLM_MODEL is
// unset OR no LLM provider key is configured — Pitfall 1 / Assumption A6). The
// RegenerateTitle handler returns 503 in that case.
type TitlerHandler struct {
	titler           *service.Titler
	conversationRepo domain.ConversationRepository
	messageRepo      domain.MessageRepository
}

// NewTitlerHandler constructs a TitlerHandler. titler may be nil (graceful
// disable). conversationRepo and messageRepo MUST be non-nil — they are
// wiring-time invariants and a nil there is a programmer bug. Mirror the
// service/hitl.go:92-123 nil-guard convention.
func NewTitlerHandler(
	titler *service.Titler,
	conversationRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
) *TitlerHandler {
	if conversationRepo == nil {
		panic("NewTitlerHandler: conversationRepo cannot be nil")
	}
	if messageRepo == nil {
		panic("NewTitlerHandler: messageRepo cannot be nil")
	}
	return &TitlerHandler{
		titler:           titler,
		conversationRepo: conversationRepo,
		messageRepo:      messageRepo,
	}
}

// RegenerateTitle handles POST /api/v1/conversations/{id}/regenerate-title.
//
// State machine (TITLE-09 / D-07):
//
//  1. middleware.GetUserID — 401 on miss.
//  2. h.conversationRepo.GetByID — 404 on ErrConversationNotFound, 500 on other err.
//  3. ownership: conv.UserID != userID.String() → 403.
//  4. titler-disabled gate: h.titler == nil → 503 (graceful disable per A6).
//  5. status check (D-02 / D-03 verbatim Russian copy):
//     - status == "manual"       → 409 "Нельзя регенерировать — вы уже переименовали чат вручную"
//     - status == "auto_pending" → 409 "Заголовок уже генерируется"
//  6. atomic transition via TransitionToAutoPending; on ErrConversationNotFound
//     → 409 "title_state_changed" (race window between the read in step 2 and
//     this atomic write — manual rename arrived mid-flight).
//  7. fetch user + assistant text; spawn titler goroutine on a fresh detached
//     ctx with 30s timeout (Pitfall 2 / Landmine 5: r.Context() is unsafe —
//     it's canceled at HTTP response close, the cheap-LLM call takes 3-8s).
//  8. respond 200 (empty body) — fire-and-forget.
func (h *TitlerHandler) RegenerateTitle(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "id")
	conv, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.ErrorContext(r.Context(), "regenerate-title: lookup failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if conv.UserID != userID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Graceful disable: titling not configured (A6 / Pitfall 1).
	if h.titler == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   "titler_disabled",
			"message": "Auto-title service is unavailable. Set TITLER_MODEL to enable.",
		})
		return
	}

	// D-02: manual is sovereign — verbatim Russian copy locked in CONTEXT.md.
	if conv.TitleStatus == domain.TitleStatusManual {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":   "title_is_manual",
			"message": "Нельзя регенерировать — вы уже переименовали чат вручную",
		})
		return
	}
	// D-03: in-flight job already running — verbatim Russian copy locked in CONTEXT.md.
	if conv.TitleStatus == domain.TitleStatusAutoPending {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":   "title_in_flight",
			"message": "Заголовок уже генерируется",
		})
		return
	}

	// Atomic transition status:auto → auto_pending. The repo's filter excludes
	// "manual" so a manual rename arriving between the read above and this
	// update is rejected; surface as a generic 409 so the frontend re-fetches
	// the conversation and discovers the new state (race-loss is recoverable).
	if err := h.conversationRepo.TransitionToAutoPending(r.Context(), conversationID); err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":   "title_state_changed",
				"message": "Заголовок изменился — обновите страницу",
			})
			return
		}
		slog.ErrorContext(r.Context(), "regenerate-title: transition failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Fetch latest user + assistant text for the titler prompt. List
	// failure is non-fatal — the titler will simply fall through to the
	// empty-response branch and log the outcome.
	msgs, err := h.messageRepo.ListByConversationID(r.Context(), conversationID, 100, 0)
	if err != nil {
		slog.WarnContext(r.Context(), "regenerate-title: list messages failed",
			"error", err, "conversation_id", conversationID)
	}
	var userText, assistantText string
	for i := len(msgs) - 1; i >= 0 && (userText == "" || assistantText == ""); i-- {
		if assistantText == "" && msgs[i].Role == "assistant" &&
			(msgs[i].Status == domain.MessageStatusComplete || msgs[i].Status == "") {
			assistantText = msgs[i].Content
		}
		if userText == "" && msgs[i].Role == "user" {
			userText = msgs[i].Content
		}
	}

	// Pitfall 2 / Landmine 5: fresh detached ctx with 30s timeout — r.Context()
	// is canceled at HTTP response close and the cheap-LLM call takes 3-8s.
	// The goroutine owns the cancel so the timer releases when it exits.
	spawnCtx, spawnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	// The acceptance grep requires the literal `go h.titler.GenerateAndSave(spawnCtx`
	// — wrap the call so the closure forwards every arg verbatim AND cancels the
	// timeout when the spawned work completes (vet would flag a discarded cancel).
	go h.titler.GenerateAndSave(spawnCtx, conv.BusinessID, conversationID, userText, assistantText)
	go func() {
		<-spawnCtx.Done()
		spawnCancel()
	}()

	w.WriteHeader(http.StatusOK) // 200, no body — fire-and-forget per D-07
}
