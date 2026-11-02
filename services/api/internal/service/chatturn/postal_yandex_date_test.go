package chatturn

import (
	"testing"

	"github.com/stretchr/testify/assert"

	platformsync "github.com/f1xgun/onevoice/services/api/internal/platform"
)

func TestReviewFromToolResult_YandexPublicationDate(t *testing.T) {
	review := reviewFromToolResult(map[string]interface{}{"id": "review", "date": "3 ноября 2025", "rating": "5"}, "business", "yandex_business")
	assert.Equal(t, 5, review.Rating)
	assert.Equal(t, "2025-11-03T00:00:00+03:00", review.PlatformMeta[platformsync.YandexReviewPublishedAtKey])
}
