package tools_test

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/tools"
)

func TestDisplayName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Full platform__action
		{"telegram__send_channel_post", "Отправить пост"},
		{"vk__publish_post", "Опубликовать пост"},
		{"yandex_business__get_reviews", "Загрузить отзывы Яндекса"},
		{"google_business__reply_review", "Ответить на отзыв Google"},

		// Bare sync-task names
		{"sync_title", "Синхронизация названия"},
		{"sync_photo", "Синхронизация фото"},

		// Unknown full name — fallback humanize on bare action
		{"telegram__future_feature_x", "Future feature x"},

		// Unknown bare name — fallback humanize
		{"brand_new_op", "Brand new op"},

		// Empty
		{"", ""},
	}
	for _, c := range cases {
		if got := tools.DisplayName(c.in); got != c.want {
			t.Errorf("DisplayName(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
