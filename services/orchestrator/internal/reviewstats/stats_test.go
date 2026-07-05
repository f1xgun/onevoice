package reviewstats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func review(rating int, status string, createdAt time.Time) domain.Review {
	return domain.Review{
		Rating:      rating,
		ReplyStatus: status,
		CreatedAt:   createdAt,
		AuthorName:  "Иван Петров",
		Text:        "Отличное место, всем рекомендую! Мой телефон +7 900 000 00 00",
		ReplyText:   "Спасибо за отзыв!",
	}
}

// TestAggregate_EmptyCollection proves a business with zero reviews returns
// honest zeros: no division-by-zero, no panic, an all-zero distribution.
func TestAggregate_EmptyCollection(t *testing.T) {
	got := Aggregate(nil, time.Now(), 7)

	assert.Equal(t, 0, got.Total)
	assert.Equal(t, 0, got.Answered)
	assert.Equal(t, 0, got.Unanswered)
	assert.Equal(t, 0.0, got.ReplyRate)
	assert.Equal(t, 0.0, got.AverageRating)
	assert.Equal(t, 0, got.RecentTotal)
	assert.Equal(t, 7, got.RecentDays)
	for star := minRating; star <= maxRating; star++ {
		assert.Equal(t, 0, got.RatingDistribution[itoa(star)], "star %d bucket must be present and zero", star)
	}
}

// TestAggregate_Math is the behavioral guard for the aggregation logic: reply
// rate, average rating, distribution, and answered/unanswered counts. Reverting
// the aggregation body breaks these assertions (fail-on-revert).
func TestAggregate_Math(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	reviews := []domain.Review{
		review(5, domain.ReviewReplyStatusReplied, now.AddDate(0, 0, -1)),
		review(5, domain.ReviewReplyStatusReplied, now.AddDate(0, 0, -2)),
		review(4, domain.ReviewReplyStatusPending, now.AddDate(0, 0, -3)),
		review(1, domain.ReviewReplyStatusError, now.AddDate(0, 0, -40)),
	}

	got := Aggregate(reviews, now, 7)

	assert.Equal(t, 4, got.Total)
	assert.Equal(t, 2, got.Answered)
	assert.Equal(t, 2, got.Unanswered)
	assert.InDelta(t, 0.5, got.ReplyRate, 1e-9)
	assert.InDelta(t, 3.75, got.AverageRating, 1e-9)
	assert.Equal(t, 2, got.RatingDistribution["5"])
	assert.Equal(t, 1, got.RatingDistribution["4"])
	assert.Equal(t, 1, got.RatingDistribution["1"])
	assert.Equal(t, 0, got.RatingDistribution["3"])
	assert.Equal(t, 0, got.RatingDistribution["2"])
}

// TestAggregate_RecentWindow proves the recent-period counts include only
// reviews inside the window and that answered-in-window is tracked separately.
func TestAggregate_RecentWindow(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	reviews := []domain.Review{
		review(5, domain.ReviewReplyStatusReplied, now.AddDate(0, 0, -1)),
		review(4, domain.ReviewReplyStatusPending, now.AddDate(0, 0, -6)),
		review(3, domain.ReviewReplyStatusReplied, now.AddDate(0, 0, -6)),
		review(2, domain.ReviewReplyStatusReplied, now.AddDate(0, 0, -30)),
	}

	got := Aggregate(reviews, now, 7)

	assert.Equal(t, 3, got.RecentTotal, "only reviews within 7 days count")
	assert.Equal(t, 2, got.RecentAnswered, "answered-in-window excludes the 30-day-old one")
}

// TestAggregate_DefaultWindow proves a non-positive window falls back to the
// package default rather than counting everything.
func TestAggregate_DefaultWindow(t *testing.T) {
	got := Aggregate(nil, time.Now(), 0)
	assert.Equal(t, defaultRecentDays, got.RecentDays)

	gotNeg := Aggregate(nil, time.Now(), -5)
	assert.Equal(t, defaultRecentDays, gotNeg.RecentDays)
}

// TestAggregate_IgnoresOutOfRangeRatings proves a 0 or 6 rating is counted in
// Total but excluded from the average and the per-star distribution.
func TestAggregate_IgnoresOutOfRangeRatings(t *testing.T) {
	now := time.Now()
	reviews := []domain.Review{
		review(5, domain.ReviewReplyStatusReplied, now),
		review(0, domain.ReviewReplyStatusPending, now),
		review(6, domain.ReviewReplyStatusPending, now),
	}

	got := Aggregate(reviews, now, 7)

	assert.Equal(t, 3, got.Total)
	assert.InDelta(t, 5.0, got.AverageRating, 1e-9, "average considers only in-range ratings")
	total := 0
	for star := minRating; star <= maxRating; star++ {
		total += got.RatingDistribution[itoa(star)]
	}
	assert.Equal(t, 1, total, "only the valid 5-star review lands in the distribution")
}

// TestStats_AggregateOnly_NoPDn proves the serialized Stats never carries raw
// review text, author names, or reply text — only numbers. The reviews fed in
// deliberately embed a name and a phone number; none may appear in the output.
func TestStats_AggregateOnly_NoPDn(t *testing.T) {
	now := time.Now()
	reviews := []domain.Review{
		review(5, domain.ReviewReplyStatusReplied, now),
		review(1, domain.ReviewReplyStatusPending, now),
	}

	blob, err := json.Marshal(Aggregate(reviews, now, 7))
	require.NoError(t, err)
	out := string(blob)

	for _, leak := range []string{"Иван", "Петров", "рекомендую", "+7 900", "Спасибо", "author", "text"} {
		assert.False(t, strings.Contains(out, leak),
			"aggregate output must not contain personal or free-text data: found %q in %s", leak, out)
	}
}
