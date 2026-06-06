package wire

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// vkTools returns the VK agent's tool specs. Verbatim copy of the VK block
// from the historical registerPlatformTools (services/orchestrator/cmd/main.go
// :372-559) — split into its own file so wire/tools.go stays under SC-01's
// 500-LOC budget.
func vkTools() []toolregistry.ToolSpec {
	return []toolregistry.ToolSpec{
		{
			DisplayName:     "Опубликовать пост",
			DisplayNameKey:  "tools.vk.publish_post.name",
			UserDescription: "Публикует пост на стене сообщества ВКонтакте.",
			DescriptionEn:   "Publishes a text post (no photo) to a VK community wall. If you need to publish a post with a photo, use vk__post_photo instead.",
			ParameterDescriptionsEn: map[string]string{
				"text":     "Post text",
				"group_id": "Community ID",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKPublishPost,
				Description: "Публикует текстовый пост (без фото) на стену сообщества ВКонтакте. Если нужно опубликовать пост с фото — используй vk__post_photo вместо этого.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text":     map[string]interface{}{"type": "string", "description": "Текст поста"},
						"group_id": map[string]interface{}{"type": "string", "description": "ID сообщества"},
					},
					"required": []string{"text"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		},
		{
			DisplayName:     "Опубликовать фото",
			DisplayNameKey:  "tools.vk.post_photo.name",
			UserDescription: "Публикует пост с фото на стене сообщества ВКонтакте.",
			DescriptionEn:   "Publishes a post with a photo and text caption to a VK community wall. Use this function instead of publish_post when you need to publish a post that includes an image.",
			ParameterDescriptionsEn: map[string]string{
				"photo_url": "Public image URL to upload",
				"caption":   "Photo caption text",
				"group_id":  "VK community ID",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKPostPhoto,
				Description: "Публикует пост с фото и текстовой подписью на стену сообщества ВКонтакте. Используй эту функцию вместо publish_post когда нужно опубликовать пост с изображением.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"photo_url": map[string]interface{}{"type": "string", "description": "Публичный URL изображения для загрузки"},
						"caption":   map[string]interface{}{"type": "string", "description": "Текстовая подпись к фото"},
						"group_id":  map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте"},
					},
					"required": []string{"photo_url"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"caption"},
		},
		{
			DisplayName:     "Запланировать пост",
			DisplayNameKey:  "tools.vk.schedule_post.name",
			UserDescription: "Планирует отложенную публикацию на стене ВКонтакте.",
			DescriptionEn:   "Schedules a delayed post on a VK community wall. The post will be automatically published by VK at the specified time.",
			ParameterDescriptionsEn: map[string]string{
				"text":         "Post text",
				"publish_date": "Publish date and time (Unix timestamp or ISO 8601 format, e.g. 2026-03-20T12:00:00Z)",
				"group_id":     "VK community ID",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKSchedulePost,
				Description: "Планирует отложенный пост на стене сообщества ВКонтакте. Пост будет автоматически опубликован ВКонтакте в указанное время.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text":         map[string]interface{}{"type": "string", "description": "Текст поста"},
						"publish_date": map[string]interface{}{"type": "string", "description": "Дата и время публикации (Unix timestamp или ISO 8601 формат, например 2026-03-20T12:00:00Z)"},
						"group_id":     map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте"},
					},
					"required": []string{"text", "publish_date"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		},
		{
			DisplayName:     "Обновить данные сообщества",
			DisplayNameKey:  "tools.vk.update_group_info.name",
			UserDescription: "Изменяет название, описание и контакты сообщества ВКонтакте.",
			DescriptionEn:   "Updates VK community info (description, links, contacts). If group_id is not provided, uses the community from the active VK integration.",
			ParameterDescriptionsEn: map[string]string{
				"group_id":    "Numeric VK community ID. Optional — taken from the active integration if omitted.",
				"description": "New description",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKUpdateGroupInfo,
				Description: "Обновляет информацию о сообществе ВКонтакте (описание, ссылки, контакты). Если group_id не указан, используется сообщество из активной VK-интеграции.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"group_id":    map[string]interface{}{"type": "string", "description": "Числовой ID сообщества ВКонтакте. Необязателен — берётся из активной интеграции."},
						"description": map[string]interface{}{"type": "string", "description": "Новое описание"},
					},
					"required": []string{},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"description"},
		},
		{
			DisplayName:     "Загрузить комментарии",
			DisplayNameKey:  "tools.vk.get_comments.name",
			UserDescription: "Загружает комментарии к посту ВКонтакте.",
			DescriptionEn:   "Fetches comments for a specific post on a VK community wall. If post_id is not provided, returns comments for the most recent post.",
			ParameterDescriptionsEn: map[string]string{
				"post_id":  "Wall post ID. If omitted — the most recent post is used.",
				"group_id": "Numeric VK community ID. Optional — taken from the active integration if omitted.",
				"count":    "Number of comments (max 100)",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKGetComments,
				Description: "Получает комментарии к конкретному посту на стене сообщества ВКонтакте. Если post_id не указан, возвращает комментарии к последнему посту.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"post_id":  map[string]interface{}{"type": "integer", "description": "ID поста на стене. Если не указан — берётся последний пост."},
						"group_id": map[string]interface{}{"type": "string", "description": "Числовой ID сообщества ВКонтакте. Необязателен — берётся из активной интеграции."},
						"count":    map[string]interface{}{"type": "integer", "description": "Количество комментариев (макс 100)"},
					},
					"required": []string{},
				},
			}},
			Floor:          domain.ToolFloorAuto,
			EditableFields: nil,
		},
		{
			DisplayName:     "Ответить на комментарий",
			DisplayNameKey:  "tools.vk.reply_comment.name",
			UserDescription: "Отвечает на комментарий ВКонтакте.",
			DescriptionEn:   "Replies to a comment on a post on a VK community wall. Creates a reply in the discussion thread.",
			ParameterDescriptionsEn: map[string]string{
				"post_id":    "Wall post ID",
				"comment_id": "ID of the comment to reply to",
				"text":       "Comment reply text",
				"group_id":   "VK community ID",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKReplyComment,
				Description: "Отвечает на комментарий к посту на стене сообщества ВКонтакте. Создает ответ в ветке обсуждения.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"post_id":    map[string]interface{}{"type": "number", "description": "ID поста на стене"},
						"comment_id": map[string]interface{}{"type": "number", "description": "ID комментария, на который нужно ответить"},
						"text":       map[string]interface{}{"type": "string", "description": "Текст ответа на комментарий"},
						"group_id":   map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте"},
					},
					"required": []string{"post_id", "comment_id", "text"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		},
		{
			DisplayName:     "Удалить комментарий",
			DisplayNameKey:  "tools.vk.delete_comment.name",
			UserDescription: "Удаляет комментарий под постом ВКонтакте.",
			DescriptionEn:   "Deletes a comment on a post on a VK community wall. Requires administrator or moderator permissions for the community.",
			ParameterDescriptionsEn: map[string]string{
				"comment_id": "ID of the comment to delete",
				"group_id":   "VK community ID",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKDeleteComment,
				Description: "Удаляет комментарий к посту на стене сообщества ВКонтакте. Требуются права администратора или модератора сообщества.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"comment_id": map[string]interface{}{"type": "number", "description": "ID комментария для удаления"},
						"group_id":   map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте"},
					},
					"required": []string{"comment_id"},
				},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: nil,
		},
		{
			DisplayName:     "Загрузить данные сообщества",
			DisplayNameKey:  "tools.vk.get_community_info.name",
			UserDescription: "Загружает карточку сообщества ВКонтакте.",
			DescriptionEn:   "Fetches VK community info: name, description, subscriber count, status, links. Use to answer questions about the community.",
			ParameterDescriptionsEn: map[string]string{
				"group_id": "VK community ID. If omitted, the community from the active VK integration is used.",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKGetCommunityInfo,
				Description: "Получает информацию о сообществе ВКонтакте: название, описание, количество подписчиков, статус, ссылки. Используй для ответа на вопросы о сообществе.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"group_id": map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте. Если не указан, используется группа из активной VK-интеграции."},
					},
					"required": []string{},
				},
			}},
			Floor:          domain.ToolFloorAuto,
			EditableFields: nil,
		},
		{
			DisplayName:     "Загрузить посты",
			DisplayNameKey:  "tools.vk.get_wall_posts.name",
			UserDescription: "Загружает посты со стены сообщества ВКонтакте.",
			DescriptionEn:   "Fetches the most recent posts from a VK community wall, with stats for likes, comments, reposts, and views.",
			ParameterDescriptionsEn: map[string]string{
				"group_id": "VK community ID. If omitted, the community from the active VK integration is used.",
				"count":    "Number of posts (default 10, max 100)",
			},
			Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
				Name:        tools.VKGetWallPosts,
				Description: "Получает последние посты со стены сообщества ВКонтакте с данными о лайках, комментариях, репостах и просмотрах.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"group_id": map[string]interface{}{"type": "string", "description": "ID сообщества ВКонтакте. Если не указан, используется группа из активной VK-интеграции."},
						"count":    map[string]interface{}{"type": "integer", "description": "Количество постов (по умолчанию 10, макс 100)"},
					},
					"required": []string{},
				},
			}},
			Floor:          domain.ToolFloorAuto,
			EditableFields: nil,
		},
	}
}
