package i18n

// en is the English catalog. Keys may be omitted while migration is in
// progress — TrTag falls back to the ru catalog when a key is missing here.
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
	"oauth.yandex.missing_sessionid2":   "Some sign-in data is missing — replies to reviews and photo uploads may not work without it. Copy the cookies again in full.",
	"oauth.yandex.missing_yandex_login": "The Yandex login is missing — sign-in may keep dropping without it. Include it when you copy the cookies.",

	// VK connect handlers. %s is the VK-provided error message.
	"connect.vk.invalid_token":            "Invalid token: %s",
	"connect.vk.community_unknown":        "VK didn't return a community for this token — make sure you created the key in the community admin panel",
	"connect.vk.wall_permission_missing":  "Token is missing the \"Wall\" permission — recreate the key in the community admin panel with the \"Wall\" checkbox enabled",
	"connect.vk.community_resolve_failed": "Couldn't recognize the community — check the link or numeric ID and try again",

	// Yandex cookies parser — surfaced via handler boundary mapping.
	"yandex.cookies.empty":             "Paste cookies from your browser",
	"yandex.cookies.missing_sessionid": "Session_id value not found — it's the primary cookie for Yandex login",
	"yandex.cookies.invalid_format":    "Couldn't recognize the format: not JSON, not a Cookie header, and not a Session_id value",
	"yandex.cookies.invalid_sessionid": "Session_id value looks invalid — make sure you copied it completely",
	"yandex.cookies.json_error":        "JSON error: %s",

	// Chat proxy stream-error wrapper — EN form of the persisted assistant
	// fallback rendered when the SSE stream errors out before producing text.
	// %s carries the upstream error message.
	"api.chat.stream_error_wrapper": "[Error: %s]",

	// Move-conversation handler — EN equivalents of the system note copy.
	// default_destination is the "no project" bucket label. system_message
	// is the visible system note appended to the chat after a move; %s is
	// the resolved destination name. See catalog_ru.go for the write-time
	// localization contract (we do NOT retranslate historical messages).
	"api.conversation.move.default_destination": "No project",
	"api.conversation.move.system_message":      "[Chat moved to \"%s\" — the new policy applies from this point]",

	// Password-reset error messages — EN equivalents.
	"api.password_reset.token_invalid": "This link is no longer valid. Please request a new one.",
	"api.password_reset.token_expired": "This link has expired. Please request a new one.",
	"api.password_reset.password_weak": "Password is too short — minimum 8 characters.",

	// Permission descriptions — EN equivalents of the role-editor tooltip
	// copy. The serving handler resolves these per request locale.
	"permissions.business.read.desc":               "View the organization's name, description, and settings.",
	"permissions.business.update.desc":             "Edit the name, description, and basic settings.",
	"permissions.business.delete.desc":             "Permanently delete the organization along with all its data.",
	"permissions.business.transfer_ownership.desc": "Transfer ownership to another member. Current owner only.",
	"permissions.members.read.desc":                "View the list of members and their roles.",
	"permissions.members.invite.desc":              "Create invite links for new members.",
	"permissions.members.remove.desc":              "Remove members from the organization.",
	"permissions.members.update_role.desc":         "Assign a different role to members. Except themselves.",
	"permissions.roles.read.desc":                  "View the list of roles and the permissions they hold.",
	"permissions.roles.create.desc":                "Create custom roles with a specific set of permissions.",
	"permissions.roles.update.desc":                "Edit custom roles — name, description, permissions.",
	"permissions.roles.delete.desc":                "Delete custom roles. If members hold the role, you'll need to pick a new role for them.",
	"permissions.integrations.read.desc":           "View connected platforms and their status.",
	"permissions.integrations.connect.desc":        "Connect new platforms — Telegram, VK, Yandex.Business.",
	"permissions.integrations.disconnect.desc":     "Disconnect connected platforms.",
	"permissions.content.read.desc":                "View posts, reviews, conversations, and tasks.",
	"permissions.content.create.desc":              "Create posts, reply to reviews, and assign tasks.",
	"permissions.content.update.desc":              "Edit existing posts, replies, and tasks.",
	"permissions.content.delete.desc":              "Delete posts, replies, and tasks.",
	"permissions.billing.read.desc":                "View the plan, invoices, and usage limits.",
	"permissions.billing.update.desc":              "Change the plan, billing details, and payment method.",
	"permissions.audit.read.desc":                  "View the activity log — role changes, sign-ins, platform connections.",

	// Validation messages.
	"validation.failed":        "validation failed",
	"validation.required":      "field is required",
	"validation.invalid_email": "invalid email format",
	"validation.too_short":     "value is too short",
	"validation.too_long":      "value is too long",
	"validation.generic":       "validation failed",

	// Telegram connect admin-rights guard.
	"connect.telegram.not_admin":         "Add the bot as an administrator with permission to post messages, then reconnect — it takes about a minute",
	"connect.telegram.no_post_rights":    "The bot can't post messages — grant that permission in the channel settings and reconnect, it takes about a minute",
	"connect.telegram.unreachable":       "Telegram is temporarily unavailable — try again in a minute",
	"connect.telegram.rate_limited":      "Too many requests to Telegram — wait a moment and try again",
	"connect.telegram.no_access":         "The bot has no access to this channel — add it to the channel and reconnect",
	"connect.telegram.already_connected": "This channel is already connected to another organization",
	"connect.telegram.connect_failed":    "Couldn't connect the channel — please try again",

	// Proactive connection-health owner nudge.
	"notify.connection.reconnect_yandex": "OneVoice lost access to Yandex.Business — review replies and profile updates are paused. Reconnect the organization in settings, it takes about two minutes.",
}
