package domain

import (
	"strconv"
	"strings"
)

const (
	VKUnknownUserName      = "Пользователь ВКонтакте"
	VKUnknownCommunityName = "Сообщество ВКонтакте"
)

// VKReviewAuthorName keeps a platform-provided name and replaces missing names
// or historical synthetic vk_user_<id> values with a readable fallback. The
// signed VK author ID distinguishes communities from people; it is not a name.
func VKReviewAuthorName(name string, fromID int64) string {
	name = strings.TrimSpace(name)
	if rawID, legacy := strings.CutPrefix(name, "vk_user_"); legacy {
		if id, err := strconv.ParseInt(rawID, 10, 64); err == nil {
			fromID = id
			name = ""
		}
	}
	if name != "" {
		return name
	}
	if fromID < 0 {
		return VKUnknownCommunityName
	}
	return VKUnknownUserName
}
