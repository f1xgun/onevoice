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

// Build returns a []llm.Message starting with a system message built from
// the business context, followed by the conversation history.
func Build(ctx BusinessContext, history []llm.Message) []llm.Message {
	system := buildSystemContent(ctx)
	msgs := make([]llm.Message, 0, 1+len(history))
	msgs = append(msgs, llm.Message{Role: "system", Content: system})
	msgs = append(msgs, history...)
	return msgs
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
