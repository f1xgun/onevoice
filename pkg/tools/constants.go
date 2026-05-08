// Package tools centralizes the string identifiers for tools dispatched between
// the orchestrator (LLM tool registry) and platform agents (NATS handlers).
//
// Tool naming convention: {platform}__{action} (double-underscore separator).
// The orchestrator registers these for LLM tool calling; agents switch on the
// same string when receiving NATS task envelopes.
package tools

// Telegram tools.
const (
	TelegramSendChannelPost  = "telegram__send_channel_post"
	TelegramSendChannelPhoto = "telegram__send_channel_photo"
	TelegramSendNotification = "telegram__send_notification"
	TelegramGetReviews       = "telegram__get_reviews"
	TelegramReplyToComment   = "telegram__reply_to_comment"
)

// VK tools.
const (
	VKPublishPost      = "vk__publish_post"
	VKPostPhoto        = "vk__post_photo"
	VKSchedulePost     = "vk__schedule_post"
	VKUpdateGroupInfo  = "vk__update_group_info"
	VKGetComments      = "vk__get_comments"
	VKReplyComment     = "vk__reply_comment"
	VKDeleteComment    = "vk__delete_comment"
	VKGetCommunityInfo = "vk__get_community_info"
	VKGetWallPosts     = "vk__get_wall_posts"
)

// Yandex Business tools.
const (
	YandexBusinessGetInfo     = "yandex_business__get_info"
	YandexBusinessUpdateHours = "yandex_business__update_hours"
	YandexBusinessUpdateInfo  = "yandex_business__update_info"
	YandexBusinessGetReviews  = "yandex_business__get_reviews"
	YandexBusinessReplyReview = "yandex_business__reply_review"
	YandexBusinessUploadPhoto = "yandex_business__upload_photo"
	YandexBusinessCreatePost  = "yandex_business__create_post"
	// YandexBusinessListCompanies is dispatched directly by the API
	// (yandex_connect handler) and is not exposed in the LLM tool registry.
	YandexBusinessListCompanies = "yandex_business__list_companies"
)

// Google Business tools.
const (
	GoogleBusinessGetReviews  = "google_business__get_reviews"
	GoogleBusinessReplyReview = "google_business__reply_review"
)
