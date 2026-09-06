package i18n

// ru is the canonical (default + fallback) catalog. Every translatable key
// MUST exist here. Keys missing from en/ fall back to this map at runtime.
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
	"oauth.yandex.session_expired":      "Сессия Яндекса истекла — скопируйте cookies заново и повторите подключение",
	"oauth.yandex.list_orgs_failed":     "Не удалось получить список организаций — попробуйте ещё раз",
	"oauth.yandex.missing_sessionid2":   "Не нашли часть данных для входа — без неё могут не работать ответы на отзывы и загрузка фото. Скопируйте cookies заново целиком.",
	"oauth.yandex.missing_yandex_login": "Не нашли логин Яндекса — без него вход может сбрасываться. Добавьте его при копировании cookies.",

	// VK connect handlers (services/api/internal/handler/connect/vk_*.go).
	// %s is the VK-provided error message.
	"connect.vk.invalid_token":            "Невалидный токен: %s",
	"connect.vk.community_unknown":        "VK не вернул сообщество для этого токена — проверьте, что вы создали ключ в админке сообщества",
	"connect.vk.wall_permission_missing":  "Токену не хватает прав на «Стену» — пересоздайте ключ в админке сообщества с галочкой «Стена»",
	"connect.vk.community_resolve_failed": "Не удалось распознать сообщество — проверьте ссылку или числовой ID и попробуйте ещё раз",

	// Yandex cookies parser (services/api/internal/yandexcookies/parse.go)
	// surfaced via handler boundary mapping (errors.Is on typed sentinels).
	"yandex.cookies.empty":             "Вставьте cookies из браузера",
	"yandex.cookies.missing_sessionid": "Не найдено значение Session_id — это главный cookie для входа в Яндекс",
	"yandex.cookies.invalid_format":    "Не удалось распознать формат: это не JSON, не Cookie-заголовок и не значение Session_id",
	"yandex.cookies.invalid_sessionid": "Значение Session_id выглядит некорректно — проверьте, что скопировали целиком",
	"yandex.cookies.json_error":        "Ошибка JSON: %s",

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

	// Password-reset error messages
	// (services/api/internal/handler/error_mapping.go writePasswordResetError).
	"api.password_reset.token_invalid": "Ссылка недействительна — запросите новую.",
	"api.password_reset.token_expired": "Ссылка просрочена — запросите новую.",
	"api.password_reset.password_weak": "Пароль слишком короткий — минимум 8 символов.",

	// Permission descriptions (pkg/authz/permissions.go AllPermissions).
	// Surfaced by GET /api/v1/permissions for the role-editor Info tooltip.
	// The struct's hardcoded RU stays as a safe fallback; the handler
	// resolves these per request locale.
	"permissions.business.read.desc":               "Видеть название, описание и настройки организации.",
	"permissions.business.update.desc":             "Редактировать название, описание и базовые настройки.",
	"permissions.business.delete.desc":             "Безвозвратно удалить организацию вместе со всеми данными.",
	"permissions.business.transfer_ownership.desc": "Передавать владение другому участнику. Только текущий владелец.",
	"permissions.members.read.desc":                "Видеть список участников и их роли.",
	"permissions.members.invite.desc":              "Создавать ссылки-приглашения для новых участников.",
	"permissions.members.remove.desc":              "Исключать участников из организации.",
	"permissions.members.update_role.desc":         "Назначать участникам другую роль. Кроме самих себя.",
	"permissions.roles.read.desc":                  "Видеть список ролей и какие у них права.",
	"permissions.roles.create.desc":                "Создавать свои роли с особым набором прав.",
	"permissions.roles.update.desc":                "Редактировать свои роли — название, описание, права.",
	"permissions.roles.delete.desc":                "Удалять свои роли. Если на роли есть участники, потребуется выбрать новую роль для них.",
	"permissions.integrations.read.desc":           "Видеть подключённые платформы и их статус.",
	"permissions.integrations.connect.desc":        "Подключать новые платформы — Telegram, VK, Яндекс.Бизнес.",
	"permissions.integrations.disconnect.desc":     "Отключать подключённые платформы.",
	"permissions.content.read.desc":                "Видеть посты, отзывы, переписку, задачи.",
	"permissions.content.create.desc":              "Создавать посты, отвечать на отзывы, ставить задачи.",
	"permissions.content.update.desc":              "Редактировать существующие посты, ответы, задачи.",
	"permissions.content.delete.desc":              "Удалять посты, ответы, задачи.",
	"permissions.billing.read.desc":                "Видеть тариф, счета, использование лимитов.",
	"permissions.billing.update.desc":              "Менять тариф, реквизиты, способ оплаты.",
	"permissions.audit.read.desc":                  "Видеть журнал событий — изменения ролей, входы, подключение платформ.",

	// Validation messages (services/api/internal/handler/response.go).
	"validation.failed":        "Проверка не пройдена",
	"validation.required":      "Заполните это поле",
	"validation.invalid_email": "Неверный формат адреса почты",
	"validation.too_short":     "Слишком короткое значение",
	"validation.too_long":      "Слишком длинное значение",
	"validation.generic":       "Проверка не пройдена",

	// Telegram connect admin-rights guard
	// (services/api/internal/handler/connect/telegram.go).
	"connect.telegram.not_admin":            "Добавьте бота администратором с правом публиковать сообщения и подключите снова — это займёт около минуты",
	"connect.telegram.no_post_rights":       "У бота нет права публиковать сообщения — выдайте его в настройках канала и подключите снова, это займёт около минуты",
	"connect.telegram.unreachable":          "Telegram временно недоступен — попробуйте ещё раз через минуту",
	"connect.telegram.rate_limited":         "Слишком много запросов к Telegram — подождите немного и попробуйте снова",
	"connect.telegram.no_access":            "У бота нет доступа к этому каналу — добавьте его в канал и подключите снова",
	"connect.integration.already_connected": "Эта интеграция уже подключена к другой организации",
	"connect.telegram.already_connected":    "Этот канал уже подключён к другой организации",
	"connect.telegram.connect_failed":       "Не удалось подключить канал — попробуйте ещё раз",

	// Proactive connection-health owner nudge
	// (services/api/internal/service/connhealth/worker.go).
	"notify.connection.reconnect_yandex":   "Пропал доступ к Яндекс.Бизнесу — ответы на отзывы и обновление профиля приостановлены. Переподключите организацию в настройках, это займёт около двух минут.",
	"connect.telegram.channel_unavailable": "Не удалось получить доступ к каналу. Проверьте, что канал существует, его адрес указан верно, а бот добавлен администратором с правом публикации.",
}
