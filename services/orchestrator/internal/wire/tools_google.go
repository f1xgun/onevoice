package wire

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// googleTools returns the Google Business agent's tool specs. Verbatim copy
// of the Google block from the historical registerPlatformTools (services/
// orchestrator/cmd/main.go:696-735) — split into its own file so wire/tools.go
// stays under SC-01's 500-LOC budget.
func googleTools() []toolSpec {
	return []toolSpec{
		// Read-only. Auto.
		{
			displayName:     "Загрузить отзывы Google",
			displayNameKey:  "tools.google_business.get_reviews.name",
			userDescription: "Загружает отзывы клиентов из Google Business Profile.",
			descriptionEn:   "Fetches reviews for the location from Google Business Profile. Returns a list of reviews with ratings, comments, and owner replies.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.GoogleBusinessGetReviews,
				Description: "Получает отзывы о локации из Google Business Profile. Возвращает список отзывов с рейтингами, комментариями и ответами владельца.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": map[string]interface{}{"type": "integer", "description": "Количество отзывов (макс 50)"},
					},
				},
			}},
			floor:    domain.ToolFloorAuto,
			editable: nil,
		},
		// Mutating public: review reply. text editable; review_name pinned.
		{
			displayName:     "Ответить на отзыв Google",
			displayNameKey:  "tools.google_business.reply_review.name",
			userDescription: "Отвечает на отзыв клиента в Google Business Profile.",
			descriptionEn:   "Replies to a review on Google Business Profile on behalf of the business owner.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.GoogleBusinessReplyReview,
				Description: "Отвечает на отзыв в Google Business Profile от имени владельца бизнеса.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"review_name": map[string]interface{}{"type": "string", "description": "Полное имя ресурса отзыва (поле name из google_business__get_reviews)"},
						"text":        map[string]interface{}{"type": "string", "description": "Текст ответа на отзыв"},
					},
					"required": []string{"review_name", "text"},
				},
			}},
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
	}
}
