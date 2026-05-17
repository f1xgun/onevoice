package wire

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// telegramTools returns the Telegram agent's tool specs. Verbatim copy of the
// Telegram block from the historical registerPlatformTools (services/
// orchestrator/cmd/main.go:266-371) — split into its own file so wire/tools.go
// stays under SC-01's 500-LOC budget.
func telegramTools() []toolSpec {
	return []toolSpec{
		// Mutating public: posts to a Telegram channel. text + parse_mode
		// editable; channel_id pinned from integration.
		{
			displayName:     "Отправить пост",
			displayNameKey:  "tools.telegram.send_channel_post.name",
			userDescription: "Публикует текстовое сообщение в Telegram-канале.",
			descriptionEn:   "Publishes a text message to a Telegram channel (no photo). If you need to publish a post with a photo, use telegram__send_channel_photo instead.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.TelegramSendChannelPost,
				Description: "Публикует текстовое сообщение в Telegram-канал (без фото). Если нужно опубликовать пост с фото — используй telegram__send_channel_photo вместо этого.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text":       map[string]interface{}{"type": "string", "description": "Текст сообщения"},
						"channel_id": map[string]interface{}{"type": "string", "description": "ID канала"},
					},
					"required": []string{"text"},
				},
			}},
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
		// Mutating public: posts a photo + caption. caption editable;
		// photo_url and channel_id pinned (redirecting either at edit
		// time would be a footgun).
		{
			displayName:     "Отправить фото",
			displayNameKey:  "tools.telegram.send_channel_photo.name",
			userDescription: "Публикует фото с подписью в Telegram-канале.",
			descriptionEn:   "Publishes a photo with a text caption to a Telegram channel. Use this function instead of send_channel_post when you need to publish a post that includes an image.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.TelegramSendChannelPhoto,
				Description: "Публикует пост с фото и текстовой подписью в Telegram-канал. Используй эту функцию вместо send_channel_post когда нужно опубликовать пост с изображением.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"photo_url":  map[string]interface{}{"type": "string", "description": "Публичный URL изображения"},
						"caption":    map[string]interface{}{"type": "string", "description": "Подпись к фото"},
						"channel_id": map[string]interface{}{"type": "string", "description": "ID канала"},
					},
					"required": []string{"photo_url"},
				},
			}},
			floor:    domain.ToolFloorManual,
			editable: []string{"caption"},
		},
		// DM notification to owner. text editable; recipient pinned
		// from the integration (never editable).
		{
			displayName:     "Уведомление владельцу",
			displayNameKey:  "tools.telegram.send_notification.name",
			userDescription: "Отправляет личное уведомление владельцу бизнеса в Telegram.",
			descriptionEn:   "Sends a private notification to the business owner via Telegram.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.TelegramSendNotification,
				Description: "Отправляет личное уведомление владельцу бизнеса в Telegram",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text": map[string]interface{}{"type": "string", "description": "Текст уведомления"},
					},
					"required": []string{"text"},
				},
			}},
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
		// Read-only query of recent messages. Auto, no edit needed.
		{
			displayName:     "Загрузить отзывы",
			displayNameKey:  "tools.telegram.get_reviews.name",
			userDescription: "Загружает комментарии и реакции из Telegram-канала.",
			descriptionEn:   "Fetches the most recent messages/reviews sent to the bot or channel through Telegram. Each message has message_id and chat_id fields — use them to reply via telegram__reply_to_comment.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.TelegramGetReviews,
				Description: "Получает последние сообщения/отзывы, отправленные боту или в канал через Telegram. Каждое сообщение содержит поля message_id и chat_id — используй их для ответа через telegram__reply_to_comment.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": map[string]interface{}{"type": "integer", "description": "Количество сообщений (макс 100)"},
					},
				},
			}},
			floor:    domain.ToolFloorAuto,
			editable: nil,
		},
		// Mutating public: replies to a comment. text editable;
		// message_id + chat_id + channel_id pinned (changing these
		// would redirect the reply to an unrelated conversation).
		{
			displayName:     "Ответить на комментарий",
			displayNameKey:  "tools.telegram.reply_to_comment.name",
			userDescription: "Отвечает на комментарий к посту в Telegram-канале.",
			descriptionEn:   "Replies to a specific comment or message in Telegram. Use this function when you need to reply to a comment — DO NOT use telegram__send_channel_post for comment replies.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.TelegramReplyToComment,
				Description: "Отвечает на конкретный комментарий или сообщение в Telegram. Используй эту функцию когда нужно ответить на комментарий — НЕ используй telegram__send_channel_post для ответов на комментарии.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"message_id": map[string]interface{}{"type": "integer", "description": "ID сообщения/комментария, на который отвечаем (поле message_id из telegram__get_reviews)"},
						"chat_id":    map[string]interface{}{"type": "string", "description": "ID чата/группы обсуждений, где находится комментарий (поле chat_id из telegram__get_reviews)"},
						"text":       map[string]interface{}{"type": "string", "description": "Текст ответа"},
						"channel_id": map[string]interface{}{"type": "string", "description": "ID канала (необязательно, для выбора интеграции)"},
					},
					"required": []string{"message_id", "chat_id", "text"},
				},
			}},
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
	}
}
