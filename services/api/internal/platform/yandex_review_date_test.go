package platform

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestYandexReviewPublishedAt(t *testing.T) {
	now := time.Date(2026, time.November, 3, 1, 0, 0, 0, yandexReviewLocation)
	for _, test := range []struct{ value, want string }{
		{"сегодня", "2026-11-03T00:00:00+03:00"},
		{"вчера", "2026-11-02T00:00:00+03:00"},
		{"3 ноября", "2026-11-03T00:00:00+03:00"},
		{"3 ноября 2025", "2025-11-03T00:00:00+03:00"},
		{"31 декабря", "2025-12-31T00:00:00+03:00"},
		{"  2 НОЯБРЯ 2026  ", "2026-11-02T00:00:00+03:00"},
		{"2026-11-02", "2026-11-02T00:00:00+03:00"},
		{"02.11.2026", "2026-11-02T00:00:00+03:00"},
		{"2026-11-02T21:30:00Z", "2026-11-02T21:30:00Z"},
		{"31 февраля", ""}, {"неделю назад", ""}, {"", ""},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, ok := YandexReviewPublishedAt(map[string]interface{}{"date": test.value}, now)
			assert.Equal(t, test.want != "", ok)
			if ok {
				assert.Equal(t, test.want, got.Format(time.RFC3339))
			}
		})
	}
	got, ok := YandexReviewPublishedAt(map[string]interface{}{"created_at": "2025-11-03T00:00:00Z", "date": "сегодня"}, now)
	assert.True(t, ok)
	assert.Equal(t, "2025-11-03T00:00:00Z", got.Format(time.RFC3339))
}
