package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/security"
)

// Tunables for title generation.
const (
	// titleMaxChars caps the cleaned title length in RUNES (not bytes — Russian
	// runes are multi-byte). Discretionary 60–80; chose the upper bound so
	// titles can include a parenthetical clarifier where useful.
	titleMaxChars = 80

	// titleMaxOutputTokens — research recommends 20–30; chose 30 so the cheap
	// model has a small budget for sanitize-friendly punctuation it will then
	// strip in sanitizeTitle.
	titleMaxOutputTokens = 30

	// titleTemperature — research-recommended 0.3 (deterministic enough to
	// avoid title drift across regenerations of similar inputs while still
	// producing varied wording for diverse chats).
	titleTemperature = 0.3

	// titleSystemPromptRu / titleSystemPromptEn are the cheap-model instructions.
	// "Без кавычек и точек в конце" / "No quotes or trailing punctuation"
	// pre-empts most of what sanitizeTitle would otherwise have to strip.
	// Length-target ("3–6 слов" / "3–6 words") is kept identical across
	// locales so output budgets behave the same way.
	titleSystemPromptRu = "Сформулируй короткий заголовок (3–6 слов) для этого диалога. Без кавычек и точек в конце."
	titleSystemPromptEn = "Write a short title (3–6 words) for this conversation. No quotes or trailing punctuation."
)

// titleSystemPrompt returns the cheap-model instruction in the requested
// locale (Phase D2). EN for language.English, RU otherwise — matches the
// catalog default in pkg/i18n.lookup.
func titleSystemPrompt(tag language.Tag) string {
	if tag == language.English {
		return titleSystemPromptEn
	}
	return titleSystemPromptRu
}

// userTemplate returns the per-locale "Пользователь: …\n\nАссистент: …" /
// "User: …\n\nAssistant: …" framing for the cheap-LLM input. Same shape as
// the system prompt above — kept inline rather than catalog-based because it
// is a positional fmt.Sprintf template, not a translatable user-facing string.
func userTemplate(tag language.Tag) string {
	if tag == language.English {
		return "User: %s\n\nAssistant: %s"
	}
	return "Пользователь: %s\n\nАссистент: %s"
}

// chatCaller is the canonical mocking seam for the LLM-call dependency the
// Titler holds. It is package-private (intentionally lowercase) and exists
// for two reasons:
//
//  1. *llm.Router (concrete) satisfies it implicitly via its existing
//     Chat(ctx, req) (*llm.ChatResponse, error) method — production wiring in
//     services/api/cmd/main.go passes the real Router; Go's structural typing
//     handles the conversion at the call site without any adapter.
//  2. Tests use a fakeRouter that records the ChatRequest and returns canned
//     responses, without spinning up real LLM provider stubs.
//
// This is the SINGLE SOURCE OF TRUTH for the LLM-call seam.
// Callers reference *service.Titler concretely; there is no parallel
// titlerCaller interface.
type chatCaller interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// Titler generates short titles for chats via the cheap TITLER_MODEL and
// writes them atomically via the conditional UpdateTitleIfPending repo path.
// All operations are best-effort fire-and-forget; failures degrade silently.
// The service composes pkg/security/pii so PII never reaches the cheap LLM
// endpoint and gates the post-hoc title check.
type Titler struct {
	router chatCaller
	repo   domain.ConversationRepository
	model  string
}

// NewTitler constructs a Titler. All three dependencies are mandatory — a
// nil router, a nil repo, or an empty model string is a wiring bug and is
// rejected with a panic at construction time (mirrors hitl.go:92-123).
//
// The router parameter is typed as chatCaller; *llm.Router satisfies it
// implicitly via its existing Chat method, so production callers pass the
// real Router and Go's structural typing handles the conversion.
func NewTitler(router chatCaller, repo domain.ConversationRepository, model string) *Titler {
	if router == nil {
		panic("NewTitler: router cannot be nil")
	}
	if repo == nil {
		panic("NewTitler: repo cannot be nil")
	}
	if model == "" {
		panic("NewTitler: model cannot be empty (set TITLER_MODEL or LLM_MODEL)")
	}
	return &Titler{router: router, repo: repo, model: model}
}

// GenerateAndSave runs the full auto-title pipeline:
//
//  1. Pre-redact userMsg + assistantMsg via security.RedactPII.
//  2. Build a llm.ChatRequest with Model = t.model, Tier = "background", and
//     NO Tools (titler MUST NOT tool-call).
//  3. Call t.router.Chat. On error → log + recordAttempt + return; status
//     stays auto_pending so the next complete turn re-fires.
//  4. sanitizeTitle the response. If empty → log + recordAttempt + return.
//  5. security.ContainsPIIClass on the cleaned title. On match → write the
//     terminal "Untitled chat <day> <month>" Russian short form via
//     UpdateTitleIfPending under the SAME atomic guard so a manual rename
//     mid-flight still wins.
//  6. Otherwise call UpdateTitleIfPending with the cleaned title.
//     ErrConversationNotFound → manual rename won the race (INFO-level).
//
// Caller MUST pass a long-lived ctx. The request ctx from chat_proxy.go is
// unsafe — see chat_proxy.go's persistCtx pattern. All log lines are
// strictly metadata-only — never the prompt body, the assistant text, the
// redacted text, the LLM response, or the generated title.
func (t *Titler) GenerateAndSave(ctx context.Context, businessID, conversationID, userMsg, assistantMsg string) {
	metricStart := time.Now()

	// Locale resolved from request context (set by middleware.Locale earlier
	// in the chain). Drives the cheap-model system prompt AND the PII-reject
	// terminal fallback. ctx carries the request's Accept-Language all the way
	// here because chat_proxy spawns the titler with a detached-but-correlated
	// ctx that includes the i18n key.
	tag := i18n.LocaleFromContext(ctx)

	// Pre-redact. The cheap LLM never sees raw PII.
	redactedUser := security.RedactPII(userMsg)
	redactedAssistant := security.RedactPII(assistantMsg)
	promptLen := len(redactedUser) + len(redactedAssistant)

	req := llm.ChatRequest{
		UserID: uuid.Nil, // system-level call, no rate-limit attribution
		Model:  t.model,
		Messages: []llm.Message{
			{Role: "system", Content: titleSystemPrompt(tag)},
			{Role: "user", Content: fmt.Sprintf(userTemplate(tag), redactedUser, redactedAssistant)},
		},
		MaxTokens:   titleMaxOutputTokens,
		Temperature: titleTemperature,
		Tier:        "background",
		// NO Tools — titler must not be tool-calling.
	}

	resp, err := t.router.Chat(ctx, req)
	if err != nil {
		slog.WarnContext(ctx, "auto-title: llm error",
			"conversation_id", conversationID,
			"business_id", businessID,
			"prompt_length", promptLen,
			"rejected_by", "llm_error",
			"duration_ms", time.Since(metricStart).Milliseconds(),
			"error", err,
		)
		recordAttempt("failure", "llm_error")
		return // title_status stays auto_pending; next complete turn re-fires.
	}

	respLen := len(resp.Content)
	title := sanitizeTitle(resp.Content)
	if title == "" {
		slog.WarnContext(ctx, "auto-title: empty after sanitize",
			"conversation_id", conversationID,
			"business_id", businessID,
			"prompt_length", promptLen,
			"response_length", respLen,
			"rejected_by", "empty_response",
			"duration_ms", time.Since(metricStart).Milliseconds(),
		)
		recordAttempt("failure", "empty_response")
		return
	}

	// post-hoc PII gate. Match → terminal fallback.
	if class, hit := security.ContainsPIIClass(title); hit {
		terminalTitle := untitledChatLocalized(time.Now(), tag)
		slog.WarnContext(ctx, "auto-title: pii rejected",
			"conversation_id", conversationID,
			"business_id", businessID,
			"prompt_length", promptLen,
			"response_length", respLen,
			"rejected_by", "pii_regex",
			"regex_class", class,
			"duration_ms", time.Since(metricStart).Milliseconds(),
		)
		// Write the fallback under the SAME atomic guard so a concurrent
		// manual rename still wins.
		if writeErr := t.repo.UpdateTitleIfPending(ctx, conversationID, terminalTitle); writeErr != nil {
			if errors.Is(writeErr, domain.ErrConversationNotFound) {
				// Manual rename won the race even on the terminal path;
				// log INFO since this is the trust-critical "manual sovereign"
				// outcome rather than a real error.
				slog.InfoContext(ctx, "auto-title: terminal write no-op (manual won race)",
					"conversation_id", conversationID,
					"business_id", businessID,
					"prompt_length", promptLen,
					"response_length", respLen,
					"outcome", "manual_won_race",
				)
				recordAttempt("failure", "manual_won_race")
				return
			}
			slog.WarnContext(ctx, "auto-title: terminal write failed",
				"conversation_id", conversationID,
				"business_id", businessID,
				"prompt_length", promptLen,
				"response_length", respLen,
				"rejected_by", "terminal_write_error",
				"error", writeErr,
			)
			recordAttempt("failure", "terminal_write_error")
			return
		}
		recordAttempt("failure", "pii_reject")
		return
	}

	if writeErr := t.repo.UpdateTitleIfPending(ctx, conversationID, title); writeErr != nil {
		if errors.Is(writeErr, domain.ErrConversationNotFound) {
			// MatchedCount=0 → user renamed (manual_won_race) or doc deleted.
			// INFO level: this is a feature, not a bug. Manual rename is
			// sovereign.
			slog.InfoContext(ctx, "auto-title: no-op (manual rename or deleted)",
				"conversation_id", conversationID,
				"business_id", businessID,
				"prompt_length", promptLen,
				"response_length", respLen,
				"outcome", "manual_won_race",
			)
			recordAttempt("failure", "manual_won_race")
			return
		}
		slog.WarnContext(ctx, "auto-title: persist error",
			"conversation_id", conversationID,
			"business_id", businessID,
			"prompt_length", promptLen,
			"response_length", respLen,
			"rejected_by", "persist_error",
			"error", writeErr,
		)
		recordAttempt("failure", "persist_error")
		return
	}

	slog.InfoContext(ctx, "auto-title: success",
		"conversation_id", conversationID,
		"business_id", businessID,
		"prompt_length", promptLen,
		"response_length", respLen,
		"outcome", "ok",
		"duration_ms", time.Since(metricStart).Milliseconds(),
	)
	recordAttempt("success", "ok")
}

// sanitizeTitle strips quotes, trailing punctuation, and surrounding whitespace
// from raw, then caps the result at titleMaxChars RUNES (not bytes — Russian
// runes are multi-byte; bounding by len() would cut titles mid-codepoint).
//
// The cheap-model system prompt already says "Без кавычек и точек в конце";
// this helper is the post-hoc safety net for instruction-following slips.
func sanitizeTitle(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'«»“”`)
	s = strings.TrimRight(s, ".!?;:")
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > titleMaxChars {
		runes := []rune(s)
		s = string(runes[:titleMaxChars])
	}
	return s
}

// untitledChatRussian returns the terminal-fallback title in Russian
// short form, e.g. "Untitled chat 26 апреля". Go's time.Format is English-only
// for month names; we look up the Russian genitive month name from a fixed
// 12-element table.
//
// Kept for direct-call test compatibility; new code calls untitledChatLocalized.
func untitledChatRussian(t time.Time) string {
	months := [12]string{
		"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	return fmt.Sprintf("Untitled chat %d %s", t.Day(), months[t.Month()-1])
}

// untitledChatEnglish returns the terminal-fallback title in English
// short form, e.g. "Untitled chat April 26". Day after the month so the
// reader doesn't have to mentally swap the order vs the RU form.
func untitledChatEnglish(t time.Time) string {
	months := [12]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	}
	return fmt.Sprintf("Untitled chat %s %d", months[t.Month()-1], t.Day())
}

// untitledChatLocalized dispatches to the per-locale fallback (Phase D2). The
// "Untitled chat" prefix stays English in both branches because it's the
// universally-recognized empty-state marker the FE renders as a placeholder
// (matches the frontend i18n key chats.untitledFallback shape).
func untitledChatLocalized(t time.Time, tag language.Tag) string {
	if tag == language.English {
		return untitledChatEnglish(t)
	}
	return untitledChatRussian(t)
}
