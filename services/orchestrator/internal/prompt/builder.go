package prompt

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// BusinessContext holds all data needed to build the system prompt.
//
// Locale steers BOTH the section labels ("## Бизнес" vs "## Business") AND
// the language-directive line at the end of the rules block ("Общайся на
// русском языке" vs "Respond in English"). The directive is load-bearing —
// it is the single string that flips the LLM's output language. Phase D of
// `.planning/i18n-readiness/PLAN.md` documents the rationale.
//
// Locale may be any language.Tag, including the zero Tag — Build normalizes
// it via i18n.NormalizeToSupported before any internal helper sees it, so
// "anything that isn't English" produces the Russian template (preserving
// byte-for-byte pre-i18n output for legacy callers that leave Locale unset).
type BusinessContext struct {
	Name               string
	Category           string
	Address            string
	Phone              string
	Website            string
	Description        string
	Tone               string   // e.g., "дружелюбный", "профессиональный"
	ActiveIntegrations []string // e.g., ["telegram", "vk", "google_business"]
	Now                time.Time
	// Locale drives the system-prompt language. Zero Tag = RU (legacy).
	// Populated by services/orchestrator/internal/handler/chat.go from the
	// per-request body's `locale` field or, as a fallback, from
	// i18n.LocaleFromContext(r.Context()) (LocaleResolver middleware).
	Locale language.Tag
}

// ProjectContext carries the optional project prompt layer that is appended
// after the business rules block when a chat lives inside a project. When nil,
// the builder emits the legacy business-only system message.
//
// Layering order: business context → project system prompt → conversation
// history. The project block is appended AFTER the business
// rules because LLM attention gives the last-emitted block precedence, which
// matches the UX intent: project-level instructions override general business
// rules for chats inside a project.
type ProjectContext struct {
	ID            string
	Name          string
	SystemPrompt  string
	WhitelistMode domain.WhitelistMode // drives the Ограничения инструментов hint in appendProjectBlock
	AllowedTools  []string             // listed verbatim when WhitelistMode == WhitelistModeExplicit
}

// Build returns a []llm.Message starting with a system message built from
// the business context, followed by the conversation history. When proj is
// non-nil, the system message ends with a "## Проект: {Name}" /
// "## Project: {Name}" block (per locale) after the business rules and before
// the history.
//
// Locale handling: ctx.Locale is normalized once via i18n.NormalizeToSupported
// and the resulting tag is passed down to internal helpers as an explicit
// argument. Helpers therefore trust their tag and never re-normalize — the
// "collapse arbitrary tag → Russian|English" rule lives in pkg/i18n alongside
// MatchAcceptLanguage, not scattered through prompt/.
func Build(ctx BusinessContext, proj *ProjectContext, history []llm.Message) []llm.Message {
	tag := i18n.NormalizeToSupported(ctx.Locale)
	system := buildSystemContent(ctx, tag)
	if proj != nil {
		system = appendProjectBlock(system, proj, tag)
	}
	msgs := make([]llm.Message, 0, 1+len(history))
	msgs = append(msgs, llm.Message{Role: "system", Content: system})
	msgs = append(msgs, history...)
	return msgs
}

// appendProjectBlock glues the project prompt layer onto the business-only
// system text. The header is always emitted (even when SystemPrompt is empty)
// so the LLM knows which project is in scope; this makes the transition
// visible during move-chat.
//
// When WhitelistMode is explicit or none, a follow-up
// "### Ограничения инструментов" section is emitted so the LLM knows to
// refuse/explain unavailable-platform requests instead of silently
// substituting the closest allowed tool.
//
// tag must already be one of i18n.Supported (Russian or English) — Build
// normalizes ctx.Locale before invoking this helper. Section labels and
// explanatory phrasing follow tag; the tool name list itself is rendered
// as-is (LLM tool names are not localized).
func appendProjectBlock(base string, proj *ProjectContext, tag language.Tag) string {
	var sb strings.Builder
	sb.WriteString(base)
	if !strings.HasSuffix(base, "\n") {
		sb.WriteString("\n")
	}
	if tag == language.English {
		sb.WriteString("\n## Project: ")
	} else {
		sb.WriteString("\n## Проект: ")
	}
	sb.WriteString(proj.Name)
	sb.WriteString("\n")
	if proj.SystemPrompt != "" {
		sb.WriteString(proj.SystemPrompt)
		if !strings.HasSuffix(proj.SystemPrompt, "\n") {
			sb.WriteString("\n")
		}
	}

	// Whitelist explanation. Only emitted for restrictive modes.
	switch proj.WhitelistMode {
	case domain.WhitelistModeExplicit:
		if len(proj.AllowedTools) == 0 {
			// Defensive: shouldn't happen (service layer rejects this via
			// ErrProjectWhitelistEmpty), but if it does we tell the LLM the
			// same thing WhitelistModeNone would say.
			sb.WriteString(restrictionsAllDisabled(tag))
		} else {
			sb.WriteString(restrictionsAllowedOnly(tag, proj.AllowedTools))
		}
	case domain.WhitelistModeNone:
		sb.WriteString(restrictionsAllDisabled(tag))
	case domain.WhitelistModeAll, domain.WhitelistModeInherit, "":
		// No hint — permissive whitelist modes don't constrain the LLM.
	}

	return sb.String()
}

// restrictionsAllDisabled returns the per-locale "all tools disabled" hint
// emitted under "### Ограничения инструментов" / "### Tool restrictions".
// The two language variants intentionally carry the same semantic load
// (refuse-with-explanation + anti-substitution instruction) so the LLM
// behavior is identical across locales.
func restrictionsAllDisabled(tag language.Tag) string {
	if tag == language.English {
		return "\n### Tool restrictions\n" +
			"All tools are disabled for this project. Reply with text only. " +
			"If the user requests an action, explain the restriction and suggest moving the chat to another project. " +
			"Do NOT silently substitute a channel.\n"
	}
	return "\n### Ограничения инструментов\n" +
		"В этом проекте все инструменты отключены. Отвечай только текстом. " +
		"Если пользователь просит действие, объясни ограничение и предложи перенести чат в другой проект. " +
		"НЕ подменяй канал молча.\n"
}

// restrictionsAllowedOnly returns the per-locale "only these tools allowed"
// hint. Tool names are emitted verbatim (e.g. `telegram__send_channel_post`)
// in both locales because they are stable identifiers the LLM matches against
// its tools schema, not user-facing copy.
func restrictionsAllowedOnly(tag language.Tag, allowed []string) string {
	if tag == language.English {
		return "\n### Tool restrictions\n" +
			"Only the following tools are allowed in this project: " +
			strings.Join(allowed, ", ") +
			". If the user requests an action through an unavailable channel, " +
			"politely explain that the channel is disabled for the project and suggest an allowed alternative " +
			"(or simply refuse if no alternative exists). Do NOT silently substitute a channel.\n"
	}
	return "\n### Ограничения инструментов\n" +
		"В этом проекте разрешены только: " +
		strings.Join(allowed, ", ") +
		". Если пользователь просит действие через недоступный канал, " +
		"объясни вежливо, что этот канал отключён для проекта, и предложи разрешённую альтернативу " +
		"(или просто откажись, если альтернативы нет). НЕ подменяй канал молча.\n"
}

// buildSystemContent renders the locale-appropriate system prompt skeleton.
// The two language paths are kept in lock-step: every section emitted in
// Russian has an English counterpart and vice versa, so flipping the locale
// never produces a partially-localized prompt. The trailing language-steering
// line ("Общайся на русском языке" / "Respond in English") is the single
// load-bearing string that flips the LLM's reply language — section headers
// alone are insufficient. See Phase D of `.planning/i18n-readiness/PLAN.md`.
//
// tag must already be one of i18n.Supported (Russian or English) — Build
// normalizes ctx.Locale before invoking this helper.
func buildSystemContent(ctx BusinessContext, tag language.Tag) string {
	if tag == language.English {
		return buildSystemContentEn(ctx)
	}
	return buildSystemContentRu(ctx)
}

// buildSystemContentRu is the original (and default) Russian template. Kept
// byte-for-byte identical to the pre-i18n shape so existing snapshot tests
// continue to pass and so RU-locale users see no behavioral drift after the
// i18n switch lands.
func buildSystemContentRu(ctx BusinessContext) string {
	var sb strings.Builder

	sb.WriteString("Ты — AI-ассистент для управления цифровым присутствием бизнеса.\n\n")

	fmt.Fprintf(&sb, "## Бизнес: %s\n", ctx.Name)
	if ctx.Category != "" {
		fmt.Fprintf(&sb, "Категория: %s\n", ctx.Category)
	}
	if ctx.Address != "" {
		fmt.Fprintf(&sb, "Адрес: %s\n", ctx.Address)
	}
	if ctx.Phone != "" {
		fmt.Fprintf(&sb, "Телефон: %s\n", ctx.Phone)
	}
	if ctx.Website != "" {
		fmt.Fprintf(&sb, "Сайт: %s\n", ctx.Website)
	}
	if ctx.Description != "" {
		fmt.Fprintf(&sb, "Описание: %s\n", ctx.Description)
	}

	tone := ctx.Tone
	if tone == "" {
		tone = "профессиональный"
	}
	fmt.Fprintf(&sb, "\nТон общения: %s\n", tone)

	fmt.Fprintf(&sb, "\nТекущая дата и время: %s\n", ctx.Now.Format("2006-01-02 15:04 MST"))

	if len(ctx.ActiveIntegrations) > 0 {
		sb.WriteString("\n## Активные интеграции\n")
		for _, integration := range ctx.ActiveIntegrations {
			fmt.Fprintf(&sb, "- %s\n", integration)
		}
		sb.WriteString("\nТы можешь управлять этими платформами через доступные инструменты.\n")
	} else {
		sb.WriteString("\nНет активных интеграций с платформами.\n")
	}

	sb.WriteString("\n## Правила\n")
	sb.WriteString("- Выполняй задачи самостоятельно через доступные инструменты — не объясняй план, а действуй\n")
	sb.WriteString("- Если задача неясна, задай один уточняющий вопрос, затем выполни задачу без дополнительных подтверждений\n")
	sb.WriteString("- Когда пользователь просит получить отзывы/комментарии — вызывай инструменты ДЛЯ ВСЕХ активных платформ, а не только для одной\n")
	sb.WriteString("- Частичные ошибки допустимы: сообщи об успехах и неудачах после выполнения\n")
	sb.WriteString("- Общайся на русском языке\n")

	return sb.String()
}

// buildSystemContentEn is the English counterpart of buildSystemContentRu.
// Every section header, tone-default, and rule line mirrors the Russian text
// 1:1 — keep them in sync when adding new rules so locale switching never
// produces an asymmetric prompt.
//
// The trailing "Respond in English" line is load-bearing: section labels in
// English alone do not flip LLM output language without an explicit directive.
func buildSystemContentEn(ctx BusinessContext) string {
	var sb strings.Builder

	sb.WriteString("You are an AI assistant for managing a business's digital presence.\n\n")

	fmt.Fprintf(&sb, "## Business: %s\n", ctx.Name)
	if ctx.Category != "" {
		fmt.Fprintf(&sb, "Category: %s\n", ctx.Category)
	}
	if ctx.Address != "" {
		fmt.Fprintf(&sb, "Address: %s\n", ctx.Address)
	}
	if ctx.Phone != "" {
		fmt.Fprintf(&sb, "Phone: %s\n", ctx.Phone)
	}
	if ctx.Website != "" {
		fmt.Fprintf(&sb, "Website: %s\n", ctx.Website)
	}
	if ctx.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n", ctx.Description)
	}

	tone := ctx.Tone
	if tone == "" {
		tone = "professional"
	}
	fmt.Fprintf(&sb, "\nTone: %s\n", tone)

	fmt.Fprintf(&sb, "\nCurrent date and time: %s\n", ctx.Now.Format("2006-01-02 15:04 MST"))

	if len(ctx.ActiveIntegrations) > 0 {
		sb.WriteString("\n## Active integrations\n")
		for _, integration := range ctx.ActiveIntegrations {
			fmt.Fprintf(&sb, "- %s\n", integration)
		}
		sb.WriteString("\nYou can manage these platforms through the available tools.\n")
	} else {
		sb.WriteString("\nNo active platform integrations.\n")
	}

	sb.WriteString("\n## Rules\n")
	sb.WriteString("- Perform tasks independently using the available tools — do not explain the plan, take action\n")
	sb.WriteString("- If a task is unclear, ask one clarifying question, then complete the task without further confirmations\n")
	sb.WriteString("- When the user asks for reviews/comments, call tools for ALL active platforms, not just one\n")
	sb.WriteString("- Partial errors are acceptable: report successes and failures after execution\n")
	sb.WriteString("- Respond in English\n")

	return sb.String()
}
