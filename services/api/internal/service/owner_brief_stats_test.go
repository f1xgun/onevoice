package service

import (
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestAggregateOwnerBriefStats asserts the pure aggregate computes honest counts,
// rates, average, distribution, and the recent-window buckets — the same PDn-safe
// metric set get_review_stats exposes.
func TestAggregateOwnerBriefStats(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	recent := now.AddDate(0, 0, -2)
	old := now.AddDate(0, 0, -30)

	reviews := []domain.Review{
		{Rating: 5, ReplyStatus: domain.ReviewReplyStatusReplied, CreatedAt: recent},
		{Rating: 4, ReplyStatus: "pending", CreatedAt: recent},
		{Rating: 1, ReplyStatus: "pending", CreatedAt: old},
		{Rating: 5, ReplyStatus: domain.ReviewReplyStatusReplied, CreatedAt: old},
	}

	got := aggregateOwnerBriefStats(reviews, now)

	if got.Total != 4 {
		t.Errorf("Total = %d, want 4", got.Total)
	}
	if got.Answered != 2 || got.Unanswered != 2 {
		t.Errorf("Answered/Unanswered = %d/%d, want 2/2", got.Answered, got.Unanswered)
	}
	if got.ReplyRate != 0.5 {
		t.Errorf("ReplyRate = %v, want 0.5", got.ReplyRate)
	}
	if got.AverageRating != 3.75 {
		t.Errorf("AverageRating = %v, want 3.75", got.AverageRating)
	}
	if got.RatingDistribution[5] != 2 || got.RatingDistribution[4] != 1 || got.RatingDistribution[1] != 1 {
		t.Errorf("distribution mismatch: %+v", got.RatingDistribution)
	}
	if got.RecentTotal != 2 || got.RecentAnswered != 1 {
		t.Errorf("recent buckets = %d/%d, want 2/1", got.RecentTotal, got.RecentAnswered)
	}
}

// TestAggregateOwnerBriefStats_EmptyIsHonestZeros asserts no division by zero and
// an all-zero distribution for a business with no reviews.
func TestAggregateOwnerBriefStats_EmptyIsHonestZeros(t *testing.T) {
	got := aggregateOwnerBriefStats(nil, time.Now())
	if got.Total != 0 || got.ReplyRate != 0 || got.AverageRating != 0 {
		t.Errorf("empty aggregate must be zeros, got %+v", got)
	}
	for star := briefRatingMin; star <= briefRatingMax; star++ {
		if got.RatingDistribution[star] != 0 {
			t.Errorf("distribution[%d] = %d, want 0", star, got.RatingDistribution[star])
		}
	}
}
