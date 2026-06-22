package yandex

import "testing"

func TestReplyTextMatches(t *testing.T) {
	tests := []struct {
		name      string
		posted    string
		submitted string
		want      bool
	}{
		{
			name:      "exact match",
			posted:    "Спасибо за отзыв!",
			submitted: "Спасибо за отзыв!",
			want:      true,
		},
		{
			name:      "posted wraps submitted in layout whitespace",
			posted:    "  Ответ компании:\n\tСпасибо за отзыв!  ",
			submitted: "Спасибо за отзыв!",
			want:      true,
		},
		{
			name:      "case-insensitive match",
			posted:    "СПАСИБО ЗА ОТЗЫВ",
			submitted: "спасибо за отзыв",
			want:      true,
		},
		{
			name:      "collapsed inner whitespace still matches",
			posted:    "Спасибо   за    отзыв",
			submitted: "Спасибо за отзыв",
			want:      true,
		},
		{
			name:      "different text does not match",
			posted:    "Иной текст",
			submitted: "Спасибо за отзыв",
			want:      false,
		},
		{
			name:      "empty posted does not match",
			posted:    "",
			submitted: "Спасибо за отзыв",
			want:      false,
		},
		{
			name:      "empty submitted never matches",
			posted:    "Спасибо за отзыв",
			submitted: "",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replyTextMatches(tt.posted, tt.submitted); got != tt.want {
				t.Fatalf("replyTextMatches(%q, %q) = %v, want %v", tt.posted, tt.submitted, got, tt.want)
			}
		})
	}
}
