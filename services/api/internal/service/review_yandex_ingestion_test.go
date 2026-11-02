package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/domain"
	apiplatform "github.com/f1xgun/onevoice/services/api/internal/platform"
)

func TestReviewFromMap_YandexKeepsPublicationDateSeparateFromIngestion(t *testing.T) {
	old := reviewFromMap(map[string]interface{}{"id": "old", "date": "3 ноября 2025", "rating": "5"}, "business", "yandex_business")
	assert.Equal(t, 5, old.Rating)
	invalid := reviewFromMap(map[string]interface{}{"id": "half-star", "date": "сегодня", "rating": "9"}, "business", "yandex_business")
	assert.Zero(t, invalid.Rating, "a raw BEM half-star value must not become nine stars")
	assert.Equal(t, "2025-11-03T00:00:00+03:00", old.PlatformMeta[apiplatform.YandexReviewPublishedAtKey])
	assert.Equal(t, 2025, old.CreatedAt.Year())
	unknown := reviewFromMap(map[string]interface{}{"id": "unknown", "date": "давно", "rating": 5}, "business", "yandex_business")
	assert.NotContains(t, unknown.PlatformMeta, apiplatform.YandexReviewPublishedAtKey)
	cfg := apiplatform.ReviewAutopilotFromSettings(nil)
	assert.False(t, autopilotShouldPublish(cfg, unknown, "Спасибо!", false))
	current := reviewFromMap(map[string]interface{}{"id": "new", "created_at": time.Now().Add(-time.Hour).Format(time.RFC3339), "rating": 5}, "business", "yandex_business")
	current.ReplyStatus = domain.ReviewReplyStatusPending
	assert.True(t, autopilotShouldPublish(cfg, current, "Спасибо!", false))
}
