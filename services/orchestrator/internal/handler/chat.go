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
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/sseevent"
)

// Runner abstracts orchestrator.Orchestrator.Run for test injection.
type Runner interface {
	Run(ctx context.Context, req orchestrator.RunRequest) (<-chan orchestrator.Event, error)
}

// ChatHandler serves POST /chat/{conversationID} as an SSE stream.
// See docs/orchestrator/chat-handler.md.
type ChatHandler struct {
	runner       Runner
	defaultModel string
}

// NewChatHandler builds a ChatHandler; defaultModel is used when the request omits one.
func NewChatHandler(runner Runner, defaultModel string) *ChatHandler {
	return &ChatHandler{runner: runner, defaultModel: defaultModel}
}

type historyEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the POST /chat/{conversationID} body. See docs/orchestrator/chat-handler.md.
type chatRequest struct {
	Model                string         `json:"model"`
	Message              string         `json:"message"`
	BusinessID           string         `json:"business_id"`
	BusinessName         string         `json:"business_name"`
	BusinessCategory     string         `json:"business_category"`
	BusinessAddress      string         `json:"business_address"`
	BusinessPhone        string         `json:"business_phone"`
	BusinessWebsite      string         `json:"business_website"`
	BusinessDesc         string         `json:"business_description"`
	BusinessVoiceTone    []string       `json:"business_voice_tone"`
	BusinessVoiceProfile string         `json:"business_voice_profile"`
	ActiveIntegrations   []string       `json:"active_integrations"`
	History              []historyEntry `json:"history"`

	ProjectID            string   `json:"project_id"`
	ProjectName          string   `json:"project_name"`
	ProjectSystemPrompt  string   `json:"project_system_prompt"`
	ProjectWhitelistMode string   `json:"project_whitelist_mode"`
	ProjectAllowedTools  []string `json:"project_allowed_tools"`

	UserID                   string                      `json:"user_id"`
	MessageID                string                      `json:"message_id"`
	Tier                     string                      `json:"tier"`
	BusinessApprovals        map[string]domain.ToolFloor `json:"business_approvals"`
	ProjectApprovalOverrides map[string]domain.ToolFloor `json:"project_approval_overrides"`

	Locale string `json:"locale,omitempty"`
}

// Chat handles POST /chat/{conversationID} and streams orchestrator events as SSE.
// See docs/orchestrator/chat-handler.md.
func (h *ChatHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	conversationID := chi.URLParam(r, "conversationID")
	if req.Message == "" {
		http.Error(w, `{"error":"message is required"}`, http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		req.Model = h.defaultModel
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := a2a.WithBusinessID(r.Context(), req.BusinessID)
	if corrID := r.Header.Get("X-Correlation-ID"); corrID != "" {
		ctx = logger.WithCorrelationID(ctx, corrID)
	}

	locale := resolveChatLocale(ctx, req.Locale)
	ctx = i18n.WithLocale(ctx, locale)

	biz := prompt.BusinessContext{
		Name:               req.BusinessName,
		Category:           req.BusinessCategory,
		Address:            req.BusinessAddress,
		Phone:              req.BusinessPhone,
		Website:            req.BusinessWebsite,
		Description:        req.BusinessDesc,
		Tone:               joinTone(req.BusinessVoiceTone, locale),
		VoiceProfile:       req.BusinessVoiceProfile,
		ActiveIntegrations: req.ActiveIntegrations,
		Now:                time.Now(),
		Locale:             locale,
	}

	mode := domain.WhitelistMode(req.ProjectWhitelistMode)
	if mode != "" && !domain.ValidWhitelistMode(mode) {
		slog.WarnContext(ctx, "invalid whitelist mode from proxy, falling back to inherit",
			"mode", req.ProjectWhitelistMode,
		)
		mode = ""
	}

	var projCtx *prompt.ProjectContext
	if req.ProjectID != "" {
		projCtx = &prompt.ProjectContext{
			ID:            req.ProjectID,
			Name:          req.ProjectName,
			SystemPrompt:  req.ProjectSystemPrompt,
			WhitelistMode: mode,
			AllowedTools:  req.ProjectAllowedTools,
		}
	}

	history := make([]llm.Message, 0, len(req.History)+1)
	for _, h := range req.History {
		history = append(history, llm.Message{Role: h.Role, Content: h.Content})
	}
	history = append(history, llm.Message{Role: "user", Content: req.Message})

	runReq := orchestrator.RunRequest{
		Model:                    req.Model,
		BusinessContext:          biz,
		ProjectContext:           projCtx,
		WhitelistMode:            mode,
		AllowedTools:             req.ProjectAllowedTools,
		ActiveIntegrations:       req.ActiveIntegrations,
		Messages:                 history,
		ConversationID:           conversationID,
		BusinessID:               req.BusinessID,
		ProjectID:                req.ProjectID,
		UserIDString:             req.UserID,
		MessageID:                req.MessageID,
		Tier:                     req.Tier,
		BusinessApprovals:        req.BusinessApprovals,
		ProjectApprovalOverrides: req.ProjectApprovalOverrides,
	}
	if req.UserID != "" {
		if u, err := uuid.Parse(req.UserID); err == nil {
			runReq.UserID = u
		} else {
			slog.WarnContext(ctx, "invalid user_id from proxy, leaving UserID zero",
				"user_id", req.UserID, "error", err)
		}
	}

	events, err := h.runner.Run(ctx, runReq)
	if err != nil {
		writeSSE(ctx, w, flusher, translateRunnerError(ctx, err))
		return
	}

	for event := range events {
		writeSSE(ctx, w, flusher, sseevent.FromEvent(event))
	}
}

// translateRunnerError maps Run/Resume bootstrap errors to coded SSE error events.
// ErrConversationTokenCap is NOT translated here — stepRun is the canonical
// emitter and double-emission would be a bug. See docs/orchestrator/chat-handler.md.
func translateRunnerError(ctx context.Context, err error) sse.Event {
	switch {
	case errors.Is(err, llm.ErrDailySpendExceeded):
		return sse.Event{Type: "error", Code: "daily_spend_exceeded", Content: friendlyDailySpendMessage(ctx)}
	case errors.Is(err, llm.ErrRateLimitUnavailable):
		return sse.Event{Type: "error", Code: "rate_limit_unavailable", Content: friendlyRateLimitUnavailableMessage(ctx)}
	case errors.Is(err, llm.ErrRateLimitExceeded):
		return sse.Event{Type: "error", Code: "rate_limit_exceeded", Content: friendlyRateLimitExceededMessage(ctx)}
	default:
		return sse.Event{Type: "error", Content: err.Error()}
	}
}

// Friendly two-locale fallback strings. Event.Code is the machine discriminator.
const (
	dailySpendRU        = "Достигнут дневной лимит расходов для этого бизнеса. Попробуйте завтра или свяжитесь с владельцем."
	dailySpendEN        = "Daily spend limit reached for this business. Try again tomorrow or contact the owner."
	rateLimitUnavailRU  = "Сервис ограничения запросов временно недоступен. Попробуйте позже."
	rateLimitUnavailEN  = "Rate limiter is temporarily unavailable. Please try again shortly."
	rateLimitExceededRU = "Слишком много запросов. Подождите минуту и повторите."
	rateLimitExceededEN = "Too many requests. Wait a minute and try again."
)

// friendlyDailySpendMessage returns the daily-spend cap message in the request locale.
func friendlyDailySpendMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return dailySpendEN
	}
	return dailySpendRU
}

// friendlyRateLimitUnavailableMessage returns the limiter-unavailable message in the request locale.
func friendlyRateLimitUnavailableMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return rateLimitUnavailEN
	}
	return rateLimitUnavailRU
}

// friendlyRateLimitExceededMessage returns the rate-exceeded message in the request locale.
func friendlyRateLimitExceededMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return rateLimitExceededEN
	}
	return rateLimitExceededRU
}

// writeSSE marshals event and writes a single "data: ...\n\n" frame, then flushes.
func writeSSE(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, event sse.Event) {
	data, err := sse.Marshal(event)
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal SSE event", "error", err)
		return
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		slog.ErrorContext(ctx, "SSE write failed",
			"error", err,
			"event_type", event.Type,
		)
		return
	}
	flusher.Flush()
}

// resolveChatLocale picks the language tag for the prompt builder: body field
// wins over Accept-Language so a proxy can't silently flip the LLM language.
// See docs/orchestrator/chat-handler.md.
func resolveChatLocale(ctx context.Context, bodyLocale string) language.Tag {
	if bodyLocale != "" {
		return i18n.MatchAcceptLanguage(bodyLocale)
	}
	return i18n.LocaleFromContext(ctx)
}

// toneIDToRu maps canonical tone ids (frontend lib/tones.ts) to RU prompt adjectives.
var toneIDToRu = map[string]string{
	"warm":         "тёплый",
	"calm":         "спокойный",
	"friendly":     "дружеский",
	"professional": "профессиональный",
	"playful":      "игривый",
	"businesslike": "деловой",
}

// toneIDToEn is the EN parallel of toneIDToRu; same key set so a missing EN
// value is a compile-time inconsistency, not a silent fallback.
var toneIDToEn = map[string]string{
	"warm":         "warm",
	"calm":         "calm",
	"friendly":     "friendly",
	"professional": "professional",
	"playful":      "playful",
	"businesslike": "businesslike",
}

// toneLegacyRuToRu recognizes pre-migration RU display labels still in business.settings.voiceTone.
var toneLegacyRuToRu = map[string]string{
	"тёплый":           "тёплый",
	"теплый":           "тёплый",
	"спокойный":        "спокойный",
	"дружеский":        "дружеский",
	"профессиональный": "профессиональный",
	"игривый":          "игривый",
	"деловой":          "деловой",
}

// toneLegacyRuToID bridges legacy RU labels back to canonical ids for the EN path.
var toneLegacyRuToID = map[string]string{
	"тёплый":           "warm",
	"теплый":           "warm",
	"спокойный":        "calm",
	"дружеский":        "friendly",
	"профессиональный": "professional",
	"игривый":          "playful",
	"деловой":          "businesslike",
}

// toneLabel resolves a single tone token (id or legacy RU) to the per-locale
// adjective; returns "" if unknown so joinTone drops it.
func toneLabel(token string, tag language.Tag) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(token))
	if key == "" {
		return "", false
	}
	if tag == language.English {
		if v, ok := toneIDToEn[key]; ok {
			return v, true
		}
		if id, ok := toneLegacyRuToID[key]; ok {
			return toneIDToEn[id], true
		}
		return "", false
	}
	if v, ok := toneIDToRu[key]; ok {
		return v, true
	}
	if v, ok := toneLegacyRuToRu[key]; ok {
		return v, true
	}
	return "", false
}

// joinTone resolves stored tone identifiers into a deduplicated, comma-separated
// phrase in the requested locale; empty result defers to the builder's default.
func joinTone(tags []string, tag language.Tag) string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		label, ok := toneLabel(t, tag)
		if !ok {
			continue
		}
		if _, dup := seen[label]; dup {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return strings.Join(out, ", ")
}
