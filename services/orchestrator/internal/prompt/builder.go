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
//
// Build is retained as a back-compat wrapper over BuildSplit for callers that
// still want a single concatenated system message (titler, draft_reply). New
// callers should prefer BuildSplit + llm.ChatRequest.SystemBlocks for the
// two-block Anthropic prompt cache split (Plan 24-02).
func Build(ctx BusinessContext, proj *ProjectContext, history []llm.Message) []llm.Message {
	platform, business, msgs := BuildSplit(ctx, proj, history)
	system := platform + "\n" + business
	out := make([]llm.Message, 0, 1+len(msgs))
	out = append(out, llm.Message{Role: "system", Content: system})
	out = append(out, msgs...)
	return out
}

// BuildSplit returns the two-block system prompt — Block 1 (platform-wide,
// byte-stable per locale) and Block 2 (per-business, varies per request) —
// along with the conversation history (no leading system message).
//
// Block 1 (platform):
//   - Preamble ("Ты — AI-ассистент..." / "You are an AI assistant...")
//   - "## Правила" / "## Rules" with the platform-wide rule list
//   - Trailing language directive ("Общайся на русском языке" / "Respond in English")
//
// Block 2 (business):
//   - "## Бизнес:" / "## Business:" header + business details (Name, Category,
//     Address, Phone, Website, Description)
//   - "Тон общения:" / "Tone:"
//   - "Текущая дата и время:" / "Current date and time:"
//   - "## Активные интеграции" / "## Active integrations" list
//   - Optional "## Проект: ..." / "## Project: ..." block when proj != nil
//
// Anthropic stamps cache_control on Block 1 ONLY — see the CacheBoundary flag
// on llm.SystemBlock and stampSystemCacheControl in pkg/llm/providers/anthropic.go.
// Block 1 byte-stability across BusinessContext variations is guarded by
// TestSystemPromptHash_Stability (builder_stability_test.go).
//
// Reordering note (Plan 24-02): the legacy single-block builder emitted
// preamble → Business → Tone → Now → Integrations → Rules. Splitting it
// reorders to preamble → Rules → Business → Tone → Now → Integrations
// because the cache prefix MUST be platform-only. RESEARCH §Pattern 2:
// rules-first ordering is in fact preferred — platform rules lead the prompt
// and carry the cache_control marker.
func BuildSplit(ctx BusinessContext, proj *ProjectContext, history []llm.Message) (platform, business string, msgs []llm.Message) {
	tag := i18n.NormalizeToSupported(ctx.Locale)
	platform = buildPlatformBlock(tag)
	business = buildBusinessBlock(ctx, tag)
	if proj != nil {
		business = appendProjectBlock(business, proj, tag)
	}
	msgs = make([]llm.Message, 0, len(history))
	msgs = append(msgs, history...)
	return platform, business, msgs
}

// buildPlatformBlock renders the locale-fixed platform prefix (Block 1). It
// takes NO BusinessContext — its output is byte-stable per locale, which is
// the invariant Anthropic's cross-business prompt cache depends on.
func buildPlatformBlock(tag language.Tag) string {
	if tag == language.English {
		return buildPlatformBlockEn()
	}
	return buildPlatformBlockRu()
}

// buildPlatformBlockRu is the Russian platform prefix. The trailing language
// directive "Общайся на русском языке" is load-bearing (see Phase D of
// .planning/i18n-readiness/PLAN.md) and MUST remain at the end of Block 1
// so it appears just before the per-business business block in the
// concatenated prompt.
func buildPlatformBlockRu() string {
	var sb strings.Builder
	sb.WriteString("Ты — AI-ассистент для управления цифровым присутствием бизнеса.\n")
	sb.WriteString("\n## Правила\n")
	sb.WriteString("- Выполняй задачи самостоятельно через доступные инструменты — не объясняй план, а действуй\n")
	sb.WriteString("- Если задача неясна, задай один уточняющий вопрос, затем выполни задачу без дополнительных подтверждений\n")
	sb.WriteString("- Когда пользователь просит получить отзывы/комментарии — вызывай инструменты ДЛЯ ВСЕХ активных платформ, а не только для одной\n")
	sb.WriteString("- Частичные ошибки допустимы: сообщи об успехах и неудачах после выполнения\n")
	sb.WriteString("- Общайся на русском языке\n")
	return sb.String()
}

// buildPlatformBlockEn is the English platform prefix. See buildPlatformBlockRu
// for the load-bearing-directive invariant.
func buildPlatformBlockEn() string {
	var sb strings.Builder
	sb.WriteString("You are an AI assistant for managing a business's digital presence.\n")
	sb.WriteString("\n## Rules\n")
	sb.WriteString("- Perform tasks independently using the available tools — do not explain the plan, take action\n")
	sb.WriteString("- If a task is unclear, ask one clarifying question, then complete the task without further confirmations\n")
	sb.WriteString("- When the user asks for reviews/comments, call tools for ALL active platforms, not just one\n")
	sb.WriteString("- Partial errors are acceptable: report successes and failures after execution\n")
	sb.WriteString("- Respond in English\n")
	return sb.String()
}

// buildBusinessBlock renders the per-business Block 2. Everything that varies
// per request (business fields, integrations, current time) lives here so it
// never participates in the cache prefix.
func buildBusinessBlock(ctx BusinessContext, tag language.Tag) string {
	if tag == language.English {
		return buildBusinessBlockEn(ctx)
	}
	return buildBusinessBlockRu(ctx)
}

// buildBusinessBlockRu emits the Russian per-business section.
func buildBusinessBlockRu(ctx BusinessContext) string {
	var sb strings.Builder
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
	return sb.String()
}

// buildBusinessBlockEn emits the English per-business section.
func buildBusinessBlockEn(ctx BusinessContext) string {
	var sb strings.Builder
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
	return sb.String()
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

