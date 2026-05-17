package wire

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// vkTools returns the VK agent's tool specs. Verbatim copy of the VK block
// from the historical registerPlatformTools (services/orchestrator/cmd/main.go
// :372-559) — split into its own file so wire/tools.go stays under SC-01's
// 500-LOC budget.
func vkTools() []toolSpec {
	return []toolSpec{
		// Mutating public: publishes wall post. text editable; group_id pinned.
		{
			displayName:     "Опубликовать пост",
			displayNameKey:  "tools.vk.publish_post.name",
			userDescription: "Публикует пост на стене сообщества ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
		// Mutating public: photo + caption. caption editable; photo_url + group_id pinned.
		{
			displayName:     "Опубликовать фото",
			displayNameKey:  "tools.vk.post_photo.name",
			userDescription: "Публикует пост с фото на стене сообщества ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"caption"},
		},
		// Mutating public w/ scheduled release. text editable;
		// publish_date NOT editable (changing a scheduled time is a
		// semantic change — a separate tool call makes intent explicit).
		{
			displayName:     "Запланировать пост",
			displayNameKey:  "tools.vk.schedule_post.name",
			userDescription: "Планирует отложенную публикацию на стене ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
		// Mutating public: group meta update. description editable;
		// group_id pinned. Contacts/links intentionally omitted from
		// edit-allowlist until the LLM's JSON schema exposes them.
		{
			displayName:     "Обновить данные сообщества",
			displayNameKey:  "tools.vk.update_group_info.name",
			userDescription: "Изменяет название, описание и контакты сообщества ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"description"},
		},
		// Read-only. Auto.
		{
			displayName:     "Загрузить комментарии",
			displayNameKey:  "tools.vk.get_comments.name",
			userDescription: "Загружает комментарии к посту ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorAuto,
			editable: nil,
		},
		// Mutating public: comment reply. text editable; ids pinned.
		{
			displayName:     "Ответить на комментарий",
			displayNameKey:  "tools.vk.reply_comment.name",
			userDescription: "Отвечает на комментарий ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: []string{"text"},
		},
		// Destructive: hard-deletes a comment. Manual-floor — legitimate
		// moderation use-case (spam, abuse). Default behavior is
		// per-call approval; a business owner can bump to auto if
		// they trust the LLM's filtering. No editable fields:
		// comment_id is a hard ID that users must not override at
		// approval time (redirect-delete attack).
		{
			displayName:     "Удалить комментарий",
			displayNameKey:  "tools.vk.delete_comment.name",
			userDescription: "Удаляет комментарий под постом ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorManual,
			editable: nil,
		},
		// Read-only. Auto.
		{
			displayName:     "Загрузить данные сообщества",
			displayNameKey:  "tools.vk.get_community_info.name",
			userDescription: "Загружает карточку сообщества ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorAuto,
			editable: nil,
		},
		// Read-only. Auto.
		{
			displayName:     "Загрузить посты",
			displayNameKey:  "tools.vk.get_wall_posts.name",
			userDescription: "Загружает посты со стены сообщества ВКонтакте.",
			def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
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
			floor:    domain.ToolFloorAuto,
			editable: nil,
		},
	}
}
