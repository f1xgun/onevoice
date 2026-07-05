package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
)

// briefMaxOutputTokens caps the composed brief so a runaway generation cannot
// blow the metered budget; a weekly summary fits comfortably inside this.
const briefMaxOutputTokens = 400

// briefTemperature keeps the composed brief varied but grounded in the supplied
// numbers.
const briefTemperature = 0.4

// briefSystemPromptRu and briefSystemPromptEn instruct the model to write the
// weekly brief in the org's brand voice from the supplied AGGREGATE numbers
// only. The prompt never carries author names, review text, or reply text, so
// no personal data reaches the provider.
const (
	briefSystemPromptRu = "Ты — ассистент, который пишет короткую еженедельную сводку для владельца организации на основе агрегированных цифр по отзывам. Пиши в фирменном стиле организации, тёплым деловым тоном. Только цифры и выводы: не выдумывай имена, тексты отзывов или ответов — их у тебя нет. 3–5 предложений."
	briefSystemPromptEn = "You are an assistant writing a short weekly summary for a business owner from aggregate review numbers. Write in the organization's brand voice, warm and businesslike. Use only the numbers and takeaways: do not invent author names, review text, or reply text — you do not have them. 3–5 sentences."
)

// OwnerBriefRouter is the LLM-call seam the brief composer depends on;
// *llm.Router satisfies it implicitly. It is exported so wire can inject the
// shared titler Router (built WithBilling, so metering fires) without leaking a
// concrete type into the service constructor. Kept narrow so tests can inject a
// fake that records the request (to assert metering + PDn-safety of the prompt)
// or returns an error (to exercise the templated fallback).
type OwnerBriefRouter interface {
	Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

// composeBrief produces the weekly brief text for a business. It first attempts
// an AI-composed brief in the business's brand voice, metered through the
// existing WithBilling router by setting ChatRequest.BusinessID. On ANY router
// error (a credit/daily-spend denial via llm.ErrRateLimitExceeded, a provider
// outage, or an empty response) it degrades to the deterministic ru/en template
// — never a silent no-send, never an uncharged forced spend. usedLLM reports
// which path produced the text so telemetry can attribute mode=llm|template.
//
// includeOptOut appends the one-tap opt-out line; the caller passes true for the
// first brief (no prior send stamped) so the owner always learns how to turn it
// off. The prompt is built from stats (aggregate numbers) only — biz supplies
// the display name and brand voice, never any review author/text/reply field.
func composeBrief(ctx context.Context, router OwnerBriefRouter, model string, biz *domain.Business, stats OwnerBriefStats, tag language.Tag, includeOptOut bool) (text string, usedLLM bool) {
	if router == nil || model == "" {
		return templatedBrief(biz, stats, tag, includeOptOut), false
	}

	req := llm.ChatRequest{
		UserID:     uuid.Nil,
		BusinessID: biz.ID,
		Model:      model,
		Messages: []llm.Message{
			{Role: "system", Content: briefSystemPrompt(tag) + "\n\n" + brandVoiceInstruction(biz, tag)},
			{Role: "user", Content: briefStatsPrompt(biz, stats, tag)},
		},
		MaxTokens:   briefMaxOutputTokens,
		Temperature: briefTemperature,
		Tier:        "background",
	}

	resp, err := router.Chat(ctx, req)
	if err != nil || resp == nil || strings.TrimSpace(resp.Content) == "" {
		return templatedBrief(biz, stats, tag, includeOptOut), false
	}

	body := strings.TrimSpace(resp.Content)
	if includeOptOut {
		body += "\n\n" + optOutLine(tag)
	}
	return body, true
}

// brandVoiceInstruction folds the per-business voiceProfile override into the
// system prompt when one is stored, so the composed brief honors the same brand
// voice the chat loop and review drafter use. Empty when unset.
func brandVoiceInstruction(biz *domain.Business, tag language.Tag) string {
	profile := platform.VoiceProfileFromSettings(biz.Settings)
	if profile == "" {
		if tag == language.English {
			return "No explicit brand voice is set; use a neutral, friendly business tone."
		}
		return "Фирменный стиль не задан; используй нейтральный, дружелюбный деловой тон."
	}
	if tag == language.English {
		return "Brand voice profile to follow:\n" + profile
	}
	return "Фирменный стиль организации:\n" + profile
}

// briefSystemPrompt returns the composer instruction for the requested locale.
func briefSystemPrompt(tag language.Tag) string {
	if tag == language.English {
		return briefSystemPromptEn
	}
	return briefSystemPromptRu
}

// briefStatsPrompt renders the aggregate-only numbers block fed to the composer.
// It contains ONLY the business display name and aggregate counts/rates — no
// author names, review text, or reply text ever reach this string, so the
// composer prompt is PDn-safe by construction.
func briefStatsPrompt(biz *domain.Business, stats OwnerBriefStats, tag language.Tag) string {
	if tag == language.English {
		return fmt.Sprintf(
			"Organization: %s\nTotal reviews: %d\nAnswered: %d\nUnanswered: %d\nReply rate: %.0f%%\nAverage rating: %.2f\nRating distribution (stars: count): %s\nNew reviews in the last %d days: %d (answered: %d)",
			biz.Name, stats.Total, stats.Answered, stats.Unanswered,
			stats.ReplyRate*briefCentiScale, stats.AverageRating,
			distributionString(stats.RatingDistribution),
			stats.RecentDays, stats.RecentTotal, stats.RecentAnswered,
		)
	}
	return fmt.Sprintf(
		"Организация: %s\nВсего отзывов: %d\nОтвечено: %d\nБез ответа: %d\nДоля ответов: %.0f%%\nСредняя оценка: %.2f\nРаспределение оценок (звёзды: количество): %s\nНовых отзывов за %d дней: %d (из них с ответом: %d)",
		biz.Name, stats.Total, stats.Answered, stats.Unanswered,
		stats.ReplyRate*briefCentiScale, stats.AverageRating,
		distributionString(stats.RatingDistribution),
		stats.RecentDays, stats.RecentTotal, stats.RecentAnswered,
	)
}

// distributionString renders the rating distribution in ascending star order as
// "1:0, 2:1, …, 5:12" for the numbers block. Fixed 1..5 iteration keeps the
// order stable across map-iteration nondeterminism.
func distributionString(dist map[int]int) string {
	parts := make([]string, 0, briefRatingMax-briefRatingMin+1)
	for star := briefRatingMin; star <= briefRatingMax; star++ {
		parts = append(parts, fmt.Sprintf("%s:%d", starKey(star), dist[star]))
	}
	return strings.Join(parts, ", ")
}

// templatedBrief renders the deterministic ru/en fallback brief from the same
// aggregate numbers. It is the cost/tier degrade path: when the metered LLM call
// is unavailable or credit-denied the owner still receives a real, honest
// summary rather than silence. The opt-out line is appended when includeOptOut
// is set (the first brief).
func templatedBrief(biz *domain.Business, stats OwnerBriefStats, tag language.Tag, includeOptOut bool) string {
	var b strings.Builder
	if tag == language.English {
		fmt.Fprintf(&b, "Weekly summary for %s\n\n", biz.Name)
		fmt.Fprintf(&b, "New reviews in the last %d days: %d", stats.RecentDays, stats.RecentTotal)
		if stats.RecentTotal > 0 {
			fmt.Fprintf(&b, " (%d already answered)", stats.RecentAnswered)
		}
		b.WriteString(".\n")
		fmt.Fprintf(&b, "Total reviews: %d, unanswered: %d.\n", stats.Total, stats.Unanswered)
		if stats.Total > 0 {
			fmt.Fprintf(&b, "Average rating: %.2f, reply rate: %.0f%%.\n", stats.AverageRating, stats.ReplyRate*briefCentiScale)
		}
		if stats.Unanswered > 0 {
			b.WriteString("Tip: replying to the reviews still waiting for an answer keeps your reputation strong.\n")
		}
	} else {
		fmt.Fprintf(&b, "Еженедельная сводка для «%s»\n\n", biz.Name)
		fmt.Fprintf(&b, "Новых отзывов за %d дней: %d", stats.RecentDays, stats.RecentTotal)
		if stats.RecentTotal > 0 {
			fmt.Fprintf(&b, " (из них с ответом: %d)", stats.RecentAnswered)
		}
		b.WriteString(".\n")
		fmt.Fprintf(&b, "Всего отзывов: %d, без ответа: %d.\n", stats.Total, stats.Unanswered)
		if stats.Total > 0 {
			fmt.Fprintf(&b, "Средняя оценка: %.2f, доля ответов: %.0f%%.\n", stats.AverageRating, stats.ReplyRate*briefCentiScale)
		}
		if stats.Unanswered > 0 {
			b.WriteString("Совет: ответьте на отзывы, которые ещё ждут ответа, — это поддерживает репутацию.\n")
		}
	}
	if includeOptOut {
		b.WriteString("\n" + optOutLine(tag))
	}
	return strings.TrimRight(b.String(), "\n")
}

// optOutLine is the one-tap opt-out sentence the first brief must carry so the
// owner immediately knows how to turn the weekly brief off.
func optOutLine(tag language.Tag) string {
	if tag == language.English {
		return "You can turn off these weekly summaries in your organization settings."
	}
	return "Отключить еженедельные сводки можно в настройках организации."
}
