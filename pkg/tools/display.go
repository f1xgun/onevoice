// Package tools provides shared helpers for working with agent tool names.
package tools

import (
	"strings"
	"unicode"
)

// labels maps known tool identifiers (both "platform__action" and bare
// sync-task names) to human-readable Russian labels shown in the UI.
var labels = map[string]string{
	// Telegram
	"telegram__send_channel_post":  "Отправить пост",
	"telegram__send_channel_photo": "Отправить фото",
	"telegram__send_notification":  "Уведомление владельцу",
	"telegram__get_reviews":        "Загрузить отзывы",
	"telegram__reply_to_comment":   "Ответить на комментарий",

	// VK
	"vk__publish_post":       "Опубликовать пост",
	"vk__post_photo":         "Опубликовать фото",
	"vk__schedule_post":      "Запланировать пост",
	"vk__update_group_info":  "Обновить данные сообщества",
	"vk__get_comments":       "Загрузить комментарии",
	"vk__reply_comment":      "Ответить на комментарий",
	"vk__delete_comment":     "Удалить комментарий",
	"vk__get_community_info": "Загрузить данные сообщества",
	"vk__get_wall_posts":     "Загрузить посты",

	// Yandex Business
	"yandex_business__get_info":     "Загрузить карточку организации",
	"yandex_business__update_hours": "Обновить часы работы",
	"yandex_business__update_info":  "Обновить данные организации",
	"yandex_business__get_reviews":  "Загрузить отзывы Яндекса",
	"yandex_business__reply_review": "Ответить на отзыв Яндекса",
	"yandex_business__upload_photo": "Загрузить фото",
	"yandex_business__create_post":  "Опубликовать пост в Яндекс Бизнес",

	// Google Business
	"google_business__get_reviews":  "Загрузить отзывы Google",
	"google_business__reply_review": "Ответить на отзыв Google",

	// Background sync tasks (stored with bare type in DB, platform separately)
	"sync_title":       "Синхронизация названия",
	"sync_description": "Синхронизация описания",
	"sync_photo":       "Синхронизация фото",
	"sync_info":        "Синхронизация данных",
}

// DisplayName returns a human-readable label for a tool identifier.
// It accepts both full "platform__action" names and bare action names.
// Unknown names are humanized (snake_case → Title case) as a safe fallback.
func DisplayName(tool string) string {
	if tool == "" {
		return ""
	}
	if v, ok := labels[tool]; ok {
		return v
	}
	if i := strings.Index(tool, "__"); i != -1 {
		bare := tool[i+2:]
		if v, ok := labels[bare]; ok {
			return v
		}
		return humanize(bare)
	}
	return humanize(tool)
}

// humanize turns "send_channel_post" into "Send channel post".
func humanize(name string) string {
	if name == "" {
		return ""
	}
	s := strings.ReplaceAll(name, "_", " ")
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
