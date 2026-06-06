package wire

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// telegramTools returns the Telegram agent's tool specs. Verbatim copy of the
// Telegram block from the historical registerPlatformTools (services/
// orchestrator/cmd/main.go:266-371) — split into its own file so wire/tools.go
// stays under SC-01's 500-LOC budget.
func telegramTools() []toolregistry.ToolSpec {
	return []toolregistry.ToolSpec{
		{
			DisplayName:     "Отправить пост",
			DisplayNameKey:  "tools.telegram.send_channel_post.name",
			UserDescription: "Публикует текстовое сообщение в Telegram-канале.",
			DescriptionEn:   "Publishes a text message to a Telegram channel (no photo). If you need to publish a post with a photo, use telegram__send_channel_photo instead.",
			ParameterDescriptionsEn: map[string]string{
				"text":       "Message text",
				"channel_id": "Channel ID",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		},
		{
			DisplayName:     "Отправить фото",
			DisplayNameKey:  "tools.telegram.send_channel_photo.name",
			UserDescription: "Публикует фото с подписью в Telegram-канале.",
			DescriptionEn:   "Publishes a photo with a text caption to a Telegram channel. Use this function instead of send_channel_post when you need to publish a post that includes an image.",
			ParameterDescriptionsEn: map[string]string{
				"photo_url":  "Public image URL",
				"caption":    "Photo caption",
				"channel_id": "Channel ID",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"caption"},
		},
		{
			DisplayName:     "Уведомление владельцу",
			DisplayNameKey:  "tools.telegram.send_notification.name",
			UserDescription: "Отправляет личное уведомление владельцу бизнеса в Telegram.",
			DescriptionEn:   "Sends a private notification to the business owner via Telegram.",
			ParameterDescriptionsEn: map[string]string{
				"text": "Notification text",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		},
		{
			DisplayName:     "Загрузить отзывы",
			DisplayNameKey:  "tools.telegram.get_reviews.name",
			UserDescription: "Загружает комментарии и реакции из Telegram-канала.",
			DescriptionEn:   "Fetches the most recent messages/reviews sent to the bot or channel through Telegram. Each message has message_id and chat_id fields — use them to reply via telegram__reply_to_comment.",
			ParameterDescriptionsEn: map[string]string{
				"limit": "Number of messages (max 100)",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.TelegramGetReviews,
				Description: "Получает последние сообщения/отзывы, отправленные боту или в канал через Telegram. Каждое сообщение содержит поля message_id и chat_id — используй их для ответа через telegram__reply_to_comment.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": map[string]interface{}{"type": "integer", "description": "Количество сообщений (макс 100)"},
					},
				},
			}},
			Floor:          domain.ToolFloorAuto,
			EditableFields: nil,
		},
		{
			DisplayName:     "Ответить на комментарий",
			DisplayNameKey:  "tools.telegram.reply_to_comment.name",
			UserDescription: "Отвечает на комментарий к посту в Telegram-канале.",
			DescriptionEn:   "Replies to a specific comment or message in Telegram. Use this function when you need to reply to a comment — DO NOT use telegram__send_channel_post for comment replies.",
			ParameterDescriptionsEn: map[string]string{
				"message_id": "ID of the message/comment being replied to (the message_id field from telegram__get_reviews)",
				"chat_id":    "ID of the chat/discussion group where the comment lives (the chat_id field from telegram__get_reviews)",
				"text":       "Reply text",
				"channel_id": "Channel ID (optional, used to select the integration)",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		},
	}
}
