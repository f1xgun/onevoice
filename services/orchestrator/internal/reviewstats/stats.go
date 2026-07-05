// Package reviewstats computes read-only, aggregate-only reputation metrics
// over a business's stored reviews. It powers the get_review_stats internal
// orchestrator tool ("Спроси о своей репутации"): the LLM calls it during a
// chat turn and verbalizes the returned numbers.
//
// Two concerns are kept apart so the math is unit-testable without a database:
// Aggregate is a pure function over a slice of reviews; Repo is the thin
// Mongo-backed fetcher that feeds it. Nothing here returns raw review text,
// author names, or any other personal data — only counts, averages, and rates.
package reviewstats

import (
	"math"
	"strconv"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// minRating and maxRating bound the star scale used for the distribution.
// Ratings outside this range are counted in Total but excluded from the
// per-star distribution and the average (they are not valid stars).
const (
	minRating = 1
	maxRating = 5
)

// defaultRecentDays is the look-back window for the "recent" period metrics
// ("за неделю"). It is a plain default; the tool may pass another value.
const defaultRecentDays = 7

// Stats is the aggregate-only projection returned to the LLM. Every field is a
// number: never review text, author names, or any personal data.
type Stats struct {
	Total              int            `json:"total"`
	Unanswered         int            `json:"unanswered"`
	Answered           int            `json:"answered"`
	ReplyRate          float64        `json:"reply_rate"`
	AverageRating      float64        `json:"average_rating"`
	RatingDistribution map[string]int `json:"rating_distribution"`
	RecentDays         int            `json:"recent_days"`
	RecentTotal        int            `json:"recent_total"`
	RecentAnswered     int            `json:"recent_answered"`
}

// Aggregate reduces reviews to the aggregate metric set as of now, using a
// recent-period window of recentDays days. A non-positive recentDays falls back
// to defaultRecentDays. An empty slice yields honest zeros (no division by
// zero, no crash): Total 0, ReplyRate 0, AverageRating 0, an all-zero
// distribution.
//
// answered is defined as reply_status == "replied"; every other status
// (pending, error, unset) counts as unanswered. AverageRating and the
// distribution consider only ratings within [minRating, maxRating].
func Aggregate(reviews []domain.Review, now time.Time, recentDays int) Stats {
	if recentDays <= 0 {
		recentDays = defaultRecentDays
	}

	dist := make(map[string]int, maxRating-minRating+1)
	for star := minRating; star <= maxRating; star++ {
		dist[itoa(star)] = 0
	}

	windowStart := now.AddDate(0, 0, -recentDays)
	stats := Stats{RecentDays: recentDays, RatingDistribution: dist}

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

		if r.Rating >= minRating && r.Rating <= maxRating {
			dist[itoa(r.Rating)]++
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
		stats.ReplyRate = round2(float64(stats.Answered) / float64(stats.Total))
	}
	if ratedCount > 0 {
		stats.AverageRating = round2(float64(ratingSum) / float64(ratedCount))
	}
	return stats
}

// centiScale is the factor used to round rates and averages to two decimals.
const centiScale = 100

// round2 rounds to two decimal places so rates and averages read cleanly.
func round2(v float64) float64 {
	return math.Round(v*centiScale) / centiScale
}

// itoa maps a star bucket to its string distribution key.
func itoa(star int) string {
	return strconv.Itoa(star)
}
