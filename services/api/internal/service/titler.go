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

// Tunables for title generation. See docs/services/titler.md.
const (
	titleMaxChars        = 80  // cap in RUNES (Russian runes are multi-byte).
	titleMaxOutputTokens = 30  // research-recommended budget for cheap-model titler.
	titleTemperature     = 0.3 // research-recommended low-but-not-zero.

	titleSystemPromptRu = "Сформулируй короткий заголовок (3–6 слов) для этого диалога. Без кавычек и точек в конце."
	titleSystemPromptEn = "Write a short title (3–6 words) for this conversation. No quotes or trailing punctuation."
)

// titleSystemPrompt returns the cheap-model instruction for the requested locale (EN or RU).
func titleSystemPrompt(tag language.Tag) string {
	if tag == language.English {
		return titleSystemPromptEn
	}
	return titleSystemPromptRu
}

// userTemplate returns the per-locale "User: … / Assistant: …" fmt.Sprintf framing.
func userTemplate(tag language.Tag) string {
	if tag == language.English {
		return "User: %s\n\nAssistant: %s"
	}
	return "Пользователь: %s\n\nАссистент: %s"
}

// chatCaller is the package-private LLM-call mocking seam; *llm.Router satisfies it implicitly.
// See docs/services/titler.md.
type chatCaller interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// Titler generates and atomically persists short chat titles via the cheap TITLER_MODEL.
// See docs/services/titler.md.
type Titler struct {
	router chatCaller
	repo   domain.ConversationRepository
	model  string
}

// NewTitler constructs a Titler; nil router/repo or empty model panics (wiring-bug guard).
// See docs/services/titler.md.
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

// GenerateAndSave runs the full best-effort auto-title pipeline (redact → LLM → sanitize → PII gate → atomic write).
// Caller MUST pass a long-lived ctx (see chat_proxy.go persistCtx pattern); logs are metadata-only.
// See docs/services/titler.md.
func (t *Titler) GenerateAndSave(ctx context.Context, businessID, conversationID, userMsg, assistantMsg string) {
	metricStart := time.Now()

	tag := i18n.LocaleFromContext(ctx)

	redactedUser := security.RedactPII(userMsg)
	redactedAssistant := security.RedactPII(assistantMsg)
	promptLen := len(redactedUser) + len(redactedAssistant)

	bizID := uuid.Nil
	if businessID != "" {
		if parsed, perr := uuid.Parse(businessID); perr == nil {
			bizID = parsed
		} else {
			slog.WarnContext(ctx, "auto-title: malformed business_id, billing will be skipped",
				"business_id", businessID, "error", perr)
		}
	}

	req := llm.ChatRequest{
		UserID:     uuid.Nil,
		BusinessID: bizID,
		Model:      t.model,
		Messages: []llm.Message{
			{Role: "system", Content: titleSystemPrompt(tag)},
			{Role: "user", Content: fmt.Sprintf(userTemplate(tag), redactedUser, redactedAssistant)},
		},
		MaxTokens:   titleMaxOutputTokens,
		Temperature: titleTemperature,
		Tier:        "background",
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
		return
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
		if writeErr := t.repo.UpdateTitleIfPending(ctx, conversationID, terminalTitle); writeErr != nil {
			if errors.Is(writeErr, domain.ErrConversationNotFound) {
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

// sanitizeTitle strips quotes/trailing punctuation/whitespace and caps at titleMaxChars RUNES.
// See docs/services/titler.md.
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

// untitledChatRussian returns the RU terminal-fallback title (kept for test compatibility).
// See docs/services/titler.md.
func untitledChatRussian(t time.Time) string {
	months := [12]string{
		"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	return fmt.Sprintf("Untitled chat %d %s", t.Day(), months[t.Month()-1])
}

// untitledChatEnglish returns the EN terminal-fallback title (e.g. "Untitled chat April 26").
func untitledChatEnglish(t time.Time) string {
	months := [12]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	}
	return fmt.Sprintf("Untitled chat %s %d", months[t.Month()-1], t.Day())
}

// untitledChatLocalized dispatches to the per-locale terminal-fallback ("Untitled chat" prefix stays EN).
// See docs/services/titler.md.
func untitledChatLocalized(t time.Time, tag language.Tag) string {
	if tag == language.English {
		return untitledChatEnglish(t)
	}
	return untitledChatRussian(t)
}
