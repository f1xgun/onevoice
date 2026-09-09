package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVKReviewAuthorName(t *testing.T) {
	for _, tt := range []struct {
		name   string
		fromID int64
		want   string
	}{
		{"  Иван Петров  ", 123, "Иван Петров"},
		{"Студия", -123, "Студия"},
		{"", 123, VKUnknownUserName},
		{"  ", -123, VKUnknownCommunityName},
		{"vk_user_240508926", 0, VKUnknownUserName},
		{"vk_user_-123", 0, VKUnknownCommunityName},
		{"vk_user_artist", 123, "vk_user_artist"},
	} {
		t.Run(tt.name, func(t *testing.T) { assert.Equal(t, tt.want, VKReviewAuthorName(tt.name, tt.fromID)) })
	}
}
