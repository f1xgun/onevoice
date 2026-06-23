package yandex

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

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

func TestValidateReviewID(t *testing.T) {
	tests := []struct {
		name     string
		reviewID string
		wantErr  bool
	}{
		{name: "numeric opaque id", reviewID: "123456789012345", wantErr: false},
		{name: "alphanumeric with dash and underscore", reviewID: "rev-9aF_3", wantErr: false},
		{name: "base64-ish chars stay valid", reviewID: "aB3.d+e/f=", wantErr: false},
		{name: "single-quote selector breakout", reviewID: "x'] , [data-x='", wantErr: true},
		{name: "double quote", reviewID: `x"y`, wantErr: true},
		{name: "backslash escape", reviewID: `x\y`, wantErr: true},
		{name: "backquote", reviewID: "x`y", wantErr: true},
		{name: "embedded space", reviewID: "12 34", wantErr: true},
		{name: "newline", reviewID: "12\n34", wantErr: true},
		{name: "tab", reviewID: "12\t34", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReviewID(tt.reviewID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateReviewID(%q) err = %v, wantErr = %v", tt.reviewID, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, &a2a.NonRetryableError{}) {
				t.Fatalf("validateReviewID(%q) must return a non-retryable error, got %v", tt.reviewID, err)
			}
		})
	}
}
