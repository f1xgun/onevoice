package prompt

import (
	"fmt"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// BusinessContext holds all data needed to build the system prompt.
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
}

// ProjectContext carries the optional project prompt layer that is appended
// after the business rules block when a chat lives inside a project. When nil,
// the builder emits the legacy business-only system message (pre-Phase-15
// behavior).
//
// Scope: see .planning/phases/15-projects-foundation/15-CONTEXT.md (D-09 —
// prompt layering order is business context → project system prompt →
// conversation history). The project block is appended AFTER the business
// rules because LLM attention gives the last-emitted block precedence, which
// matches the UX intent: project-level instructions override general business
// rules for chats inside a project.
type ProjectContext struct {
	ID           string
	Name         string
	SystemPrompt string
}

// Build returns a []llm.Message starting with a system message built from
// the business context, followed by the conversation history. When proj is
// non-nil, the system message ends with a "## Проект: {Name}" block after the
// business rules and before the history.
func Build(ctx BusinessContext, proj *ProjectContext, history []llm.Message) []llm.Message {
	system := buildSystemContent(ctx)
	if proj != nil {
		system = appendProjectBlock(system, proj)
	}
	msgs := make([]llm.Message, 0, 1+len(history))
	msgs = append(msgs, llm.Message{Role: "system", Content: system})
	msgs = append(msgs, history...)
	return msgs
}

// appendProjectBlock glues the project prompt layer onto the business-only
// system text. The header is always emitted (even when SystemPrompt is empty)
// so the LLM knows which project is in scope; this makes the transition
// visible during move-chat in Plan 04 (see PITFALLS.md §11, Option A).
func appendProjectBlock(base string, proj *ProjectContext) string {
	var sb strings.Builder
	sb.WriteString(base)
	if !strings.HasSuffix(base, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n## Проект: ")
	sb.WriteString(proj.Name)
	sb.WriteString("\n")
	if proj.SystemPrompt != "" {
		sb.WriteString(proj.SystemPrompt)
		if !strings.HasSuffix(proj.SystemPrompt, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func buildSystemContent(ctx BusinessContext) string {
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
