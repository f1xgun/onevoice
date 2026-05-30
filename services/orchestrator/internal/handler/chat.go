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

// Runner is the interface the handler depends on (allows test injection).
type Runner interface {
	Run(ctx context.Context, req orchestrator.RunRequest) (<-chan orchestrator.Event, error)
}

// ChatHandler handles SSE chat requests.
type ChatHandler struct {
	runner       Runner
	defaultModel string
}

// NewChatHandler creates a ChatHandler. defaultModel is used when the request
// does not specify a model (typically the LLM_MODEL env var).
func NewChatHandler(runner Runner, defaultModel string) *ChatHandler {
	return &ChatHandler{runner: runner, defaultModel: defaultModel}
}

type historyEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model              string         `json:"model"`
	Message            string         `json:"message"`
	BusinessID         string         `json:"business_id"`
	BusinessName       string         `json:"business_name"`
	BusinessCategory   string         `json:"business_category"`
	BusinessAddress    string         `json:"business_address"`
	BusinessPhone      string         `json:"business_phone"`
	BusinessWebsite    string         `json:"business_website"`
	BusinessDesc       string         `json:"business_description"`
	BusinessVoiceTone  []string       `json:"business_voice_tone"`
	ActiveIntegrations []string       `json:"active_integrations"`
	History            []historyEntry `json:"history"`

	// Project enrichment fields — all optional. When ProjectID is
	// empty, the orchestrator behaves identically to a no-project chat.
	// Populated by the API's chat_proxy.go after resolving the chat's
	// project_id against the Postgres projects table.
	ProjectID            string   `json:"project_id"`
	ProjectName          string   `json:"project_name"`
	ProjectSystemPrompt  string   `json:"project_system_prompt"`
	ProjectWhitelistMode string   `json:"project_whitelist_mode"`
	ProjectAllowedTools  []string `json:"project_allowed_tools"`

	// HITL identity + policy fields. Forwarded by chat_proxy.go on
	// every request; threaded into RunRequest so the orchestrator's pause
	// path persists non-empty IDs onto pending_tool_calls.
	// Pre-fix, these were missing, which made every
	// PendingToolCallBatch.conversation_id="" and broke hydration + the
	// resolve-time business-scoped auth check.
	UserID                   string                      `json:"user_id"`
	MessageID                string                      `json:"message_id"`
	Tier                     string                      `json:"tier"`
	BusinessApprovals        map[string]domain.ToolFloor `json:"business_approvals"`
	ProjectApprovalOverrides map[string]domain.ToolFloor `json:"project_approval_overrides"`

	// Locale is the per-chat language tag (e.g. "ru", "en") forwarded by the
	// API's chat_proxy from i18n.LocaleFromContext(r.Context()). Drives the
	// orchestrator prompt builder's locale-aware templates (Phase D1). Empty
	// or invalid values fall back to the orchestrator's own Accept-Language
	// resolution from middleware.Locale → i18n.LocaleFromContext, then
	// finally to i18n.DefaultTag. The body field takes precedence over the
	// header-derived ctx tag so the chat-conversation owner's preference
	// wins even when an intermediate proxy strips/rewrites Accept-Language.
	Locale string `json:"locale,omitempty"`
}

// sseEvent shape now lives in pkg/sse.Event; the mapping from
// orchestrator.Event to that wire shape lives in
// services/orchestrator/internal/sseevent. Both writeSSE call sites in this
// package (Chat / Resume) go through the builder so the field-copy switch
// is in one place.

// Chat handles POST /chat/{conversationID} and streams SSE events.
func (h *ChatHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	// Extract the conversation ID from the URL path so RunRequest.ConversationID
	// is non-empty and the persisted PendingToolCallBatch is reachable from
	// the GET /messages hydration filter.
	conversationID := chi.URLParam(r, "conversationID")
	if req.Message == "" {
		http.Error(w, `{"error":"message is required"}`, http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		req.Model = h.defaultModel
	}

	// Set SSE headers
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

	// Resolve locale for the prompt builder. Two sources:
	//   1. req.Locale  — set explicitly by the API's chat_proxy from the
	//      browser cookie (NEXT_LOCALE) → propagated as a JSON field so the
	//      chat-conversation owner's preference wins.
	//   2. ctx tag    — set by middleware.Locale from this request's
	//      Accept-Language header; the fallback when the body field is empty
	//      or invalid.
	// The body field takes precedence so that intermediate proxies that
	// strip / rewrite Accept-Language can't silently flip the LLM's reply
	// language out from under the user. See pkg/i18n + Phase D1 of
	// `.planning/i18n-readiness/PLAN.md`.
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
		ActiveIntegrations: req.ActiveIntegrations,
		Now:                time.Now(),
		Locale:             locale,
	}

	// Deserialise whitelist mode. Empty string means "inherit" (v1.3 = all).
	// Any other value that is not one of the four defined modes is
	// logged and coerced back to inherit — never crash on bad proxy input.
	mode := domain.WhitelistMode(req.ProjectWhitelistMode)
	if mode != "" && !domain.ValidWhitelistMode(mode) {
		slog.WarnContext(ctx, "invalid whitelist mode from proxy, falling back to inherit",
			"mode", req.ProjectWhitelistMode,
		)
		mode = ""
	}

	// Project enrichment: build *prompt.ProjectContext only when the
	// proxy sent a project_id. An empty project_id means "Без проекта" — the
	// orchestrator runs with no project prompt layer.
	// WhitelistMode + AllowedTools let appendProjectBlock tell
	// the LLM about the whitelist instead of silently substituting tools.
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

	// Build message history: prior turns + current user message
	history := make([]llm.Message, 0, len(req.History)+1)
	for _, h := range req.History {
		history = append(history, llm.Message{Role: h.Role, Content: h.Content})
	}
	history = append(history, llm.Message{Role: "user", Content: req.Message})

	runReq := orchestrator.RunRequest{
		Model:              req.Model,
		BusinessContext:    biz,
		ProjectContext:     projCtx,
		WhitelistMode:      mode,
		AllowedTools:       req.ProjectAllowedTools,
		ActiveIntegrations: req.ActiveIntegrations,
		Messages:           history,
		// HITL identity fields — populated from URL + body so the
		// pause-time persistence writes non-empty values to pending_tool_calls.
		// Pre-fix, these defaulted to "" and
		// every persisted batch was unreachable by hydration.
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

// translateRunnerError converts a Run / Resume bootstrap error into the SSE
// wire shape. Rate-limiter sentinels become coded error events with a
// localized fallback message; everything else falls through with the
// untranslated err.Error() text so the existing legacy behavior is preserved.
//
// ErrConversationTokenCap is intentionally NOT translated here — the agent
// loop is the canonical emitter (stepRun emits Code="conversation_token_cap"
// directly on the event channel), and double-emission would be a bug.
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

// Friendly-message helpers — two-locale switch only. The wire shape's Code
// field is the discriminator; Content is the human-readable fallback.
const (
	dailySpendRU        = "Достигнут дневной лимит расходов для этого бизнеса. Попробуйте завтра или свяжитесь с владельцем."
	dailySpendEN        = "Daily spend limit reached for this business. Try again tomorrow or contact the owner."
	rateLimitUnavailRU  = "Сервис ограничения запросов временно недоступен. Попробуйте позже."
	rateLimitUnavailEN  = "Rate limiter is temporarily unavailable. Please try again shortly."
	rateLimitExceededRU = "Слишком много запросов. Подождите минуту и повторите."
	rateLimitExceededEN = "Too many requests. Wait a minute and try again."
)

func friendlyDailySpendMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return dailySpendEN
	}
	return dailySpendRU
}

func friendlyRateLimitUnavailableMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return rateLimitUnavailEN
	}
	return rateLimitUnavailRU
}

func friendlyRateLimitExceededMessage(ctx context.Context) string {
	if i18n.LocaleFromContext(ctx) == language.English {
		return rateLimitExceededEN
	}
	return rateLimitExceededRU
}

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

// resolveChatLocale picks the language tag for the prompt builder. Precedence:
//
//  1. bodyLocale — explicit string from the chat request body (set by the
//     API's chat_proxy from the per-user cookie). Parsed via
//     i18n.MatchAcceptLanguage so unsupported / malformed values fall back to
//     i18n.DefaultTag rather than producing a zero Tag.
//  2. ctx tag    — populated by middleware.Locale earlier in the chain from
//     this request's Accept-Language header. Used only when bodyLocale is
//     empty.
//
// Body-over-header is deliberate: a backend cookie value flips the orchestrator
// language even when an intermediate proxy strips/rewrites Accept-Language. An
// empty body field is the no-opinion signal that defers to the header path.
func resolveChatLocale(ctx context.Context, bodyLocale string) language.Tag {
	if bodyLocale != "" {
		// MatchAcceptLanguage handles single-tag input ("en") and multi-tag
		// preference lists ("en-US,en;q=0.9") uniformly, and returns
		// i18n.DefaultTag on parse failure — so we never propagate a zero Tag
		// downstream.
		return i18n.MatchAcceptLanguage(bodyLocale)
	}
	return i18n.LocaleFromContext(ctx)
}

// toneIDToRu maps stable enum ids (frontend lib/tones.ts) to the Russian
// adjective the prompt builder injects. Keep in sync with lib/tones.ts —
// id strings are the contract between FE and prompt-time vocabulary.
var toneIDToRu = map[string]string{
	"warm":         "тёплый",
	"calm":         "спокойный",
	"friendly":     "дружеский",
	"professional": "профессиональный",
	"playful":      "игривый",
	"businesslike": "деловой",
}

// toneIDToEn is the EN parallel of toneIDToRu (Phase D2). Same key set so a
// missing English value is a compile-time inconsistency, not a silent fallback
// to the literal id. Adjectives chosen to match the frontend lib/tones.ts
// English labels word-for-word.
var toneIDToEn = map[string]string{
	"warm":         "warm",
	"calm":         "calm",
	"friendly":     "friendly",
	"professional": "professional",
	"playful":      "playful",
	"businesslike": "businesslike",
}

// Legacy Russian display labels that pre-migration records may still hold
// in business.settings.voiceTone. Recognized so older businesses keep
// influencing the prompt until the next save flushes the canonical id form.
//
// EN locale also routes legacy RU labels through this map first (so we can
// recognize them) then translates the recognized id via toneIDToEn — see
// toneLabel below.
var toneLegacyRuToRu = map[string]string{
	"тёплый":           "тёплый",
	"теплый":           "тёплый",
	"спокойный":        "спокойный",
	"дружеский":        "дружеский",
	"профессиональный": "профессиональный",
	"игривый":          "игривый",
	"деловой":          "деловой",
}

// toneLegacyRuToID maps legacy RU labels back to canonical IDs so the EN path
// can find an English adjective for a business that still has Russian text in
// settings.voiceTone. Direct id maps for forward-compat; legacy-text-only
// records still translate cleanly.
var toneLegacyRuToID = map[string]string{
	"тёплый":           "warm",
	"теплый":           "warm",
	"спокойный":        "calm",
	"дружеский":        "friendly",
	"профессиональный": "professional",
	"игривый":          "playful",
	"деловой":          "businesslike",
}

// toneLabel translates a single tone token (canonical id or legacy RU label)
// into the per-locale adjective. Returns "" if the token is unknown — the
// caller filters those.
//
// EN path: first try direct id → toneIDToEn, then legacy RU → id → toneIDToEn.
// RU path: kept verbatim — toneIDToRu for ids, toneLegacyRuToRu for legacy.
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

// joinTone resolves a list of stored tone identifiers (or legacy Russian
// labels) into a single comma-separated phrase in the requested locale,
// suitable for the "Тон общения: …" / "Tone: …" line in the system prompt.
// Unknown / empty entries are dropped — when nothing remains, returns "" so
// the prompt builder falls back to its locale-appropriate default.
//
// tag drives the output language; the input format is locale-agnostic (ids
// like "warm" or legacy RU labels like "тёплый" both translate cleanly).
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
