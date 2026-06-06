package wire

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// yandexTools returns the Yandex.Business agent's tool specs. Verbatim copy
// of the Yandex block from the historical registerPlatformTools (services/
// orchestrator/cmd/main.go:560-695) — split into its own file so wire/tools.go
// stays under SC-01's 500-LOC budget.
func yandexTools() []toolregistry.ToolSpec {
	return []toolregistry.ToolSpec{
		{
			DisplayName:     "Загрузить карточку организации",
			DisplayNameKey:  "tools.yandex_business.get_info.name",
			UserDescription: "Загружает карточку организации из Яндекс.Бизнеса.",
			DescriptionEn:   "Fetches the current organization info from Yandex Business: name, phone, email, business hours, address, status.",
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessGetInfo,
				Description: "Получает текущую информацию об организации в Яндекс Бизнес: название, телефон, email, часы работы, адрес, статус.",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			}},
			Floor:          domain.ToolFloorAuto,
			EditableFields: nil,
		},
		{
			DisplayName:     "Обновить часы работы",
			DisplayNameKey:  "tools.yandex_business.update_hours.name",
			UserDescription: "Обновляет часы работы организации в Яндекс.Бизнесе.",
			DescriptionEn:   "Updates business hours in Yandex Business. Accepts a free-form schedule description.",
			ParameterDescriptionsEn: map[string]string{
				"hours": "Business hours in JSON format",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessUpdateHours,
				Description: "Обновляет часы работы в Яндекс Бизнес. Принимает описание расписания в свободном формате.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"hours": map[string]interface{}{"type": "string", "description": "Часы работы в формате JSON"},
					},
					"required": []string{"hours"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"hours"},
		},
		{
			DisplayName:     "Обновить данные организации",
			DisplayNameKey:  "tools.yandex_business.update_info.name",
			UserDescription: "Изменяет описание, телефон и сайт организации в Яндекс.Бизнесе.",
			DescriptionEn:   "Updates contact info in Yandex Business (phone, website, description).",
			ParameterDescriptionsEn: map[string]string{
				"phone":       "Phone number",
				"website":     "Website URL",
				"description": "Organization description",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessUpdateInfo,
				Description: "Обновляет контактную информацию в Яндекс Бизнес (телефон, сайт, описание)",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"phone":       map[string]interface{}{"type": "string", "description": "Номер телефона"},
						"website":     map[string]interface{}{"type": "string", "description": "URL сайта"},
						"description": map[string]interface{}{"type": "string", "description": "Описание организации"},
					},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"description"},
		},
		{
			DisplayName:     "Загрузить отзывы Яндекса",
			DisplayNameKey:  "tools.yandex_business.get_reviews.name",
			UserDescription: "Загружает отзывы клиентов с Яндекс.Бизнеса.",
			DescriptionEn:   "Fetches reviews of the organization from Yandex Business.",
			ParameterDescriptionsEn: map[string]string{
				"limit": "Number of reviews (max 50)",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessGetReviews,
				Description: "Получает отзывы об организации из Яндекс Бизнес",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": map[string]interface{}{"type": "integer", "description": "Количество отзывов (макс 50)"},
					},
				},
			}},
			Floor:          domain.ToolFloorAuto,
			EditableFields: nil,
		},
		{
			DisplayName:     "Ответить на отзыв Яндекса",
			DisplayNameKey:  "tools.yandex_business.reply_review.name",
			UserDescription: "Отвечает на отзыв клиента в Яндекс.Бизнесе.",
			DescriptionEn:   "Publishes a reply to a review on Yandex Business.",
			ParameterDescriptionsEn: map[string]string{
				"review_id": "Review ID",
				"text":      "Reply text",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessReplyReview,
				Description: "Публикует ответ на отзыв в Яндекс Бизнес",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"review_id": map[string]interface{}{"type": "string", "description": "ID отзыва"},
						"text":      map[string]interface{}{"type": "string", "description": "Текст ответа"},
					},
					"required": []string{"review_id", "text"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		},
		{
			DisplayName:     "Загрузить фото",
			DisplayNameKey:  "tools.yandex_business.upload_photo.name",
			UserDescription: "Добавляет фото в галерею карточки организации в Яндекс.Бизнесе.",
			DescriptionEn:   "Uploads a photo to Yandex Business. Categories: general, logo, services, interior, exterior, enter (entrance), goods.",
			ParameterDescriptionsEn: map[string]string{
				"photo_url": "Public image URL to upload",
				"category":  "Photo category: general, logo, services, interior, exterior, enter, goods",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessUploadPhoto,
				Description: "Загружает фото в Яндекс Бизнес. Категория: general (общее), logo (логотип), services, interior, exterior, enter (вход), goods (товары).",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"photo_url": map[string]interface{}{"type": "string", "description": "Публичный URL изображения для загрузки"},
						"category":  map[string]interface{}{"type": "string", "description": "Категория фото: general, logo, services, interior, exterior, enter, goods"},
					},
					"required": []string{"photo_url"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: nil,
		},
		{
			DisplayName:     "Опубликовать пост в Яндекс Бизнес",
			DisplayNameKey:  "tools.yandex_business.create_post.name",
			UserDescription: "Публикует пост в Яндекс.Бизнесе.",
			DescriptionEn:   "Creates a publication (post) on Yandex Business. The post appears in Yandex Search and Yandex Maps.",
			ParameterDescriptionsEn: map[string]string{
				"text": "Publication text",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessCreatePost,
				Description: "Создаёт публикацию (пост) в Яндекс Бизнес. Публикация появится в Поиске Яндекса и Яндекс Картах.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text": map[string]interface{}{"type": "string", "description": "Текст публикации"},
					},
					"required": []string{"text"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		},
	}
}
