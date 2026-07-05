package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// briefRatingMin and briefRatingMax bound the star scale used for the rating
// distribution. Ratings outside this range are counted in Total but excluded
// from the distribution and the average.
const (
	briefRatingMin = 1
	briefRatingMax = 5
)

// briefRecentDays is the look-back window for the "recent" period counts the
// weekly brief foregrounds ("за неделю").
const briefRecentDays = 7

// briefCentiScale rounds rates and averages to two decimals so they read
// cleanly in the composed and templated brief.
const briefCentiScale = 100

// OwnerBriefStats is the aggregate-only reputation projection the weekly brief
// is built from. Every field is a number: never review text, author names, or
// any other personal data. It mirrors the orchestrator reviewstats.Stats shape
// so the two surfaces expose the same PDn-safe metric set, reimplemented here
// because the api module must not import another module's internal package.
type OwnerBriefStats struct {
	Total              int
	Unanswered         int
	Answered           int
	ReplyRate          float64
	AverageRating      float64
	RatingDistribution map[int]int
	RecentDays         int
	RecentTotal        int
	RecentAnswered     int
}

// ownerBriefStatsProjection restricts the Mongo read to the three fields the
// aggregate needs. author_name, text, reply_text and every draft_* field are
// never fetched, so no personal data leaves the reviews collection on this path
// — the query is aggregate-safe by construction, not merely by what the caller
// chooses to read. This is the same guarantee get_review_stats gives.
var ownerBriefStatsProjection = bson.D{
	{Key: "rating", Value: 1},
	{Key: "reply_status", Value: 1},
	{Key: "created_at", Value: 1},
	{Key: "_id", Value: 0},
}

// OwnerBriefStatsRepo reads the reviews collection the API service also writes
// to, projecting only the aggregate-safe fields. It is the api-local mirror of
// the orchestrator's reviewstats.MongoRepo.
type OwnerBriefStatsRepo struct {
	collection *mongo.Collection
}

// NewOwnerBriefStatsRepo binds a stats repo to the reviews collection of db.
func NewOwnerBriefStatsRepo(db *mongo.Database) *OwnerBriefStatsRepo {
	return &OwnerBriefStatsRepo{collection: db.Collection("reviews")}
}

// FetchStats returns the aggregate reputation stats for businessID as of now.
// The business_id filter is the tenant boundary: the caller passes the id from
// trusted enumeration context, never an untrusted argument. An empty businessID
// is rejected so a missing id cannot fan out to a full-collection scan across
// tenants.
func (r *OwnerBriefStatsRepo) FetchStats(ctx context.Context, businessID string, now time.Time) (OwnerBriefStats, error) {
	if businessID == "" {
		return OwnerBriefStats{}, fmt.Errorf("owner brief stats: business id is required")
	}

	cursor, err := r.collection.Find(ctx,
		bson.M{"business_id": businessID},
		options.Find().SetProjection(ownerBriefStatsProjection),
	)
	if err != nil {
		return OwnerBriefStats{}, fmt.Errorf("owner brief stats: find reviews: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	reviews := make([]domain.Review, 0)
	if err := cursor.All(ctx, &reviews); err != nil {
		return OwnerBriefStats{}, fmt.Errorf("owner brief stats: decode reviews: %w", err)
	}
	return aggregateOwnerBriefStats(reviews, now), nil
}

// aggregateOwnerBriefStats reduces reviews to the aggregate metric set as of
// now, using a fixed briefRecentDays window. An empty slice yields honest zeros
// (no division by zero): Total 0, ReplyRate 0, AverageRating 0, an all-zero
// distribution. answered is reply_status == "replied"; every other status
// counts as unanswered. The average and distribution consider only ratings in
// [briefRatingMin, briefRatingMax].
func aggregateOwnerBriefStats(reviews []domain.Review, now time.Time) OwnerBriefStats {
	dist := make(map[int]int, briefRatingMax-briefRatingMin+1)
	for star := briefRatingMin; star <= briefRatingMax; star++ {
		dist[star] = 0
	}

	windowStart := now.AddDate(0, 0, -briefRecentDays)
	stats := OwnerBriefStats{RecentDays: briefRecentDays, RatingDistribution: dist}

	var ratingSum, ratedCount int
	for i := range reviews {
		r := reviews[i]
		stats.Total++

		replied := r.ReplyStatus == domain.ReviewReplyStatusReplied
		if replied {
			stats.Answered++
		} else {
			stats.Unanswered++
		}

		if r.Rating >= briefRatingMin && r.Rating <= briefRatingMax {
			dist[r.Rating]++
			ratingSum += r.Rating
			ratedCount++
		}

		if !r.CreatedAt.Before(windowStart) {
			stats.RecentTotal++
			if replied {
				stats.RecentAnswered++
			}
		}
	}

	if stats.Total > 0 {
		stats.ReplyRate = roundBrief(float64(stats.Answered) / float64(stats.Total))
	}
	if ratedCount > 0 {
		stats.AverageRating = roundBrief(float64(ratingSum) / float64(ratedCount))
	}
	return stats
}

// roundBrief rounds to two decimal places so rates and averages read cleanly.
func roundBrief(v float64) float64 {
	return math.Round(v*briefCentiScale) / briefCentiScale
}

// starKey maps a star bucket to its distribution label.
func starKey(star int) string {
	return strconv.Itoa(star)
}
