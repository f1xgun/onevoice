package i18n

// ru is the canonical (default + fallback) catalog. Every translatable key
// MUST exist here. Keys missing from en/ fall back to this map at runtime.
//
// Phase C1 migrated the first batch of handler error strings out of inline
// Russian literals; later phases (C2/C3/D) will add more keys.
//
//nolint:gosec // G101: catalog values describe sessionid/token cookies as user-visible copy, not credentials.
var ru = map[string]string{
	"test.hello": "Привет, %s",

	// Titler handler — verbatim Russian copy locked in CONTEXT.md
	// (services/api/internal/handler/titler.go).
	"api.title.conflict.manual_rename": "Нельзя регенерировать — вы уже переименовали чат вручную",
	"api.title.conflict.in_progress":   "Заголовок уже генерируется",
	"api.title.conflict.stale":         "Заголовок изменился — обновите страницу",

	// Yandex OAuth/connect handler (services/api/internal/handler/oauth/yandex_connect.go).
	"oauth.yandex.invalid_body":         "Некорректное тело запроса",
	"oauth.yandex.list_orgs_failed":     "не удалось получить список организаций — попробуйте ещё раз",
	"oauth.yandex.missing_sessionid2":   "Не найден sessionid2 — может потребоваться для записи (ответы на отзывы, загрузка фото)",
	"oauth.yandex.missing_yandex_login": "Не найден yandex_login — рекомендуется добавить для стабильной авторизации",

	// VK connect handlers (services/api/internal/handler/connect/vk_*.go).
	// %s is the VK-provided error message.
	"connect.vk.invalid_token":            "Невалидный токен: %s",
	"connect.vk.community_unknown":        "VK не вернул сообщество для этого токена — проверьте, что вы создали ключ в админке сообщества",
	"connect.vk.wall_permission_missing":  "токену не хватает прав на «Стену» — пересоздайте ключ в админке сообщества с галочкой «Стена»",
	"connect.vk.community_resolve_failed": "не удалось распознать сообщество: %s",

	// Yandex cookies parser (services/api/internal/yandexcookies/parse.go)
	// surfaced via handler boundary mapping (errors.Is on typed sentinels).
	"yandex.cookies.empty":             "вставьте cookies из браузера",
	"yandex.cookies.missing_sessionid": "не найдено значение Session_id — это главный cookie для входа в Яндекс",
	"yandex.cookies.invalid_format":    "не удалось распознать формат: это не JSON, не Cookie-заголовок и не значение Session_id",
	"yandex.cookies.invalid_sessionid": "значение Session_id выглядит некорректно — проверьте, что скопировали целиком",
	"yandex.cookies.json_error":        "ошибка JSON: %s",

	// Chat proxy stream-error wrapper
	// (services/api/internal/handler/chat_proxy.go persistAfterStream).
	// %s is the upstream error message captured from the orchestrator SSE
	// "error" event; the wrapper becomes the persisted assistant message
	// content when the stream ends without producing text.
	"api.chat.stream_error_wrapper": "[Ошибка: %s]",

	// Move-conversation handler
	// (services/api/internal/handler/conversation.go MoveConversation).
	// default_destination is the virtual bucket name used when projectId is
	// null/absent (mirrors the FE "Без проекта" pseudo-project label).
	// system_message is the visible system note appended to the chat after a
	// move; %s is the resolved destination name (real project name or the
	// default_destination value above). Localized at write-time per the
	// writer's Accept-Language — the persisted string then renders in that
	// language forever in chat history (we don't retroactively re-translate
	// historical system messages, by design).
	"api.conversation.move.default_destination": "Без проекта",
	"api.conversation.move.system_message":      "[Чат перемещён в «%s» — с этого момента применяется новая политика]",

	// Validation messages (services/api/internal/handler/response.go).
	"validation.failed":        "validation failed",
	"validation.required":      "field is required",
	"validation.invalid_email": "invalid email format",
	"validation.too_short":     "value is too short",
	"validation.too_long":      "value is too long",
	"validation.generic":       "validation failed",
}
