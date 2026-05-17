package i18n

// en is the English catalog. Keys may be omitted while migration is in
// progress — TrTag falls back to the ru catalog when a key is missing here.
//
// Phase C1 mirrors the RU keys added for handler error strings with
// idiomatic English equivalents.
//
//nolint:gosec // G101: catalog values describe sessionid/token cookies as user-visible copy, not credentials.
var en = map[string]string{
	"test.hello": "Hello, %s",

	// Titler handler — English equivalents of the verbatim RU 409 copy.
	"api.title.conflict.manual_rename": "Can't regenerate — you've already renamed this chat manually",
	"api.title.conflict.in_progress":   "Title is already being generated",
	"api.title.conflict.stale":         "Title changed — please refresh the page",

	// Yandex OAuth/connect handler.
	"oauth.yandex.invalid_body":         "Invalid request body",
	"oauth.yandex.list_orgs_failed":     "Couldn't fetch the list of organizations — please try again",
	"oauth.yandex.missing_sessionid2":   "sessionid2 cookie is missing — it may be required for write operations (review replies, photo uploads)",
	"oauth.yandex.missing_yandex_login": "yandex_login cookie is missing — recommended for stable authentication",

	// VK connect handlers. %s is the VK-provided error message.
	"connect.vk.invalid_token":            "Invalid token: %s",
	"connect.vk.community_unknown":        "VK didn't return a community for this token — make sure you created the key in the community admin panel",
	"connect.vk.wall_permission_missing":  "Token is missing the \"Wall\" permission — recreate the key in the community admin panel with the \"Wall\" checkbox enabled",
	"connect.vk.community_resolve_failed": "Couldn't recognize the community: %s",

	// Yandex cookies parser — surfaced via handler boundary mapping.
	"yandex.cookies.empty":             "Paste cookies from your browser",
	"yandex.cookies.missing_sessionid": "Session_id value not found — it's the primary cookie for Yandex login",
	"yandex.cookies.invalid_format":    "Couldn't recognize the format: not JSON, not a Cookie header, and not a Session_id value",
	"yandex.cookies.invalid_sessionid": "Session_id value looks invalid — make sure you copied it completely",
	"yandex.cookies.json_error":        "JSON error: %s",

	// Validation messages.
	"validation.failed":        "validation failed",
	"validation.required":      "field is required",
	"validation.invalid_email": "invalid email format",
	"validation.too_short":     "value is too short",
	"validation.too_long":      "value is too long",
	"validation.generic":       "validation failed",
}
