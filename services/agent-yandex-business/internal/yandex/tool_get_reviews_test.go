package yandex

import "testing"

func TestReviewExternalID(t *testing.T) {
	const (
		author = "Иван"
		date   = "5 июня"
		body   = "Отличный сервис"
	)

	tests := []struct {
		name         string
		dataReviewID string
		author       string
		date         string
		body         string
		want         string
	}{
		{
			name:         "real data-review-id is returned verbatim",
			dataReviewID: "rev-9aF_3",
			author:       author,
			date:         date,
			body:         body,
			want:         "rev-9aF_3",
		},
		{
			name:         "whitespace-only data-review-id falls back to content hash",
			dataReviewID: "   ",
			author:       author,
			date:         date,
			body:         body,
			want:         reviewExternalID("", author, date, body),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reviewExternalID(tt.dataReviewID, tt.author, tt.date, tt.body); got != tt.want {
				t.Fatalf("reviewExternalID(%q, %q, %q, %q) = %q, want %q",
					tt.dataReviewID, tt.author, tt.date, tt.body, got, tt.want)
			}
		})
	}
}

// TestReviewExternalIDContentStable is the load-bearing assertion: with no
// data-review-id the key must depend on review CONTENT, not on the card's
// position in the scrape. The same review keeps its key after a newer review is
// posted above it (shifting every older index by one), and distinct content
// yields distinct keys.
func TestReviewExternalIDContentStable(t *testing.T) {
	const (
		author = "Мария"
		date   = "10 июня"
		body   = "Быстро ответили, спасибо"
	)

	// Same content, imagined at two different list positions across sync ticks.
	first := reviewExternalID("", author, date, body)
	afterShift := reviewExternalID("", author, date, body)
	if first != afterShift {
		t.Fatalf("content-identical reviews must share a key regardless of position: %q != %q", first, afterShift)
	}

	// A position-derived fallback would have keyed on the loop index, so the
	// hash must not collapse onto the raw author or a bare ordinal.
	if first == author || first == "review-0" || first == "review-1" {
		t.Fatalf("fallback key %q must be a content hash, not a position-derived value", first)
	}

	cases := []struct {
		name           string
		author         string
		date           string
		body           string
		mustDifferFrom string
	}{
		{name: "different body", author: author, date: date, body: "Совсем другой отзыв", mustDifferFrom: first},
		{name: "different author", author: "Пётр", date: date, body: body, mustDifferFrom: first},
		{name: "different date", author: author, date: "11 июня", body: body, mustDifferFrom: first},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reviewExternalID("", tc.author, tc.date, tc.body); got == tc.mustDifferFrom {
				t.Fatalf("different content must yield a different key, but both were %q", got)
			}
		})
	}
}
