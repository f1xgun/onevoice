package wire

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// yandexTools returns the Yandex.Business agent's tool specs. Verbatim copy
// of the Yandex block from the historical registerPlatformTools (services/
// orchestrator/cmd/main.go:560-695) — split into its own file so wire/tools.go
// stays under SC-01's 500-LOC budget.
func yandexTools() []toolSpec {
	return []toolSpec{
		// Read-only. Auto.
		{
			displayName:     "Загрузить карточку организации",
			userDescription: "Загружает карточку организации из Яндекс.Бизнеса.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessGetInfo,
				Description: "Получает текущую информацию об организации в Яндекс Бизнес: название, телефон, email, часы работы, адрес, статус.",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			}},
			floor:    domain.ToolFloorAuto,
			editable: nil,
		},
		// Mutating public: hours. hours editable (text payload).
		{
			displayName:     "Обновить часы работы",
			userDescription: "Обновляет часы работы организации в Яндекс.Бизнесе.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"hours"},
		},
		// Mutating public: business-profile text fields. description
		// editable. phone + website pinned (dialing/URL redirection is
		// a high-impact mutation; operator confirms via UI toggle before
		// any tool call rather than post-hoc edit).
		{
			displayName:     "Обновить данные организации",
			userDescription: "Изменяет описание, телефон и сайт организации в Яндекс.Бизнесе.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"description"},
		},
		// Read-only. Auto.
		{
			displayName:     "Загрузить отзывы Яндекса",
			userDescription: "Загружает отзывы клиентов с Яндекс.Бизнеса.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.YandexBusinessGetReviews,
				Description: "Получает отзывы об организации из Яндекс Бизнес",
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
		// Mutating public: review reply. text editable; review_id pinned.
		{
			displayName:     "Ответить на отзыв Яндекса",
			userDescription: "Отвечает на отзыв клиента в Яндекс.Бизнесе.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
		// Mutating public: upload photo. Nothing editable (category
		// and photo_url are both semantic — editing either changes
		// what the operator sees in the card vs what actually uploads).
		{
			displayName:     "Загрузить фото",
			userDescription: "Добавляет фото в галерею карточки организации в Яндекс.Бизнесе.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: nil,
		},
		// Mutating public: publication. text editable.
		{
			displayName:     "Опубликовать пост в Яндекс Бизнес",
			userDescription: "Публикует пост в Яндекс.Бизнесе.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
	}
}
