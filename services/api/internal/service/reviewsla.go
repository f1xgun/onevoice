package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// SLADefaultTargetHours is the answered-within-target window used when the
// caller supplies no target. 24 hours is the common "reply within a day"
// reputation-management SLA.
const SLADefaultTargetHours = 24

// slaBucketAgeThresholds are the age boundaries (from created_at to now) that
// partition unanswered reviews into the lt24h / h24to72 / gt72h buckets.
const (
	slaBucket24h = 24 * time.Hour
	slaBucket72h = 72 * time.Hour
)

// slaCentiScale rounds hours to two decimals so the wire numbers read cleanly.
const slaCentiScale = 100

// UnansweredBuckets counts unanswered reviews by how long they have waited,
// measured created_at → now. A review counts as unanswered when its
// ReplyStatus is not "replied" (pending, error, or unset).
type UnansweredBuckets struct {
	Lt24h   int `json:"lt24h"`
	H24to72 int `json:"h24to72"`
	Gt72h   int `json:"gt72h"`
}

// SLAStats is the aggregate-only response-SLA projection. Every field is a
// number: never review text, author names, or any personal data.
type SLAStats struct {
	Total       int               `json:"total"`
	Unanswered  int               `json:"unanswered"`
	Answered    int               `json:"answered"`
	Buckets     UnansweredBuckets `json:"buckets"`
	TargetHours int               `json:"targetHours"`

	// MedianResponseHours / AverageResponseHours are computed created_at →
	// replied_at over reviews that carry a replied_at only. Reviews answered
	// before the field existed (replied_at nil) and unanswered reviews are
	// excluded from the math rather than treated as instant, so the numbers
	// describe the reviews we can actually measure.
	MedianResponseHours  float64 `json:"medianResponseHours"`
	AverageResponseHours float64 `json:"averageResponseHours"`

	// MeasuredResponses is the count of replied reviews with a replied_at (the
	// denominator behind the median/average), so a caller can tell a genuine
	// zero from "nothing measurable yet".
	MeasuredResponses int `json:"measuredResponses"`

	// PercentAnsweredWithinTarget is the share of MEASURED responses whose
	// created_at → replied_at latency was within TargetHours, in [0,1]. It is
	// scoped to measured responses so an un-timestamped legacy reply neither
	// helps nor hurts the rate.
	PercentAnsweredWithinTarget float64 `json:"percentAnsweredWithinTarget"`
}

// computeSLA reduces reviews to the response-SLA metric set as of now, using a
// target-answered window of targetHours. A non-positive targetHours falls back
// to SLADefaultTargetHours. An empty slice yields honest zeros (no division by
// zero, no crash).
//
// answered is ReplyStatus == "replied"; every other status counts as
// unanswered and is bucketed by created_at → now age. Response-time metrics
// (median, average, percent-within-target) consider only reviews that carry a
// replied_at and whose replied_at is not before created_at (a clock-skew guard).
func computeSLA(reviews []domain.Review, now time.Time, targetHours int) SLAStats {
	if targetHours <= 0 {
		targetHours = SLADefaultTargetHours
	}
	target := time.Duration(targetHours) * time.Hour

	stats := SLAStats{TargetHours: targetHours}
	latencies := make([]time.Duration, 0, len(reviews))
	var latencySum time.Duration
	var withinTarget int

	for i := range reviews {
		r := reviews[i]
		stats.Total++

		if r.ReplyStatus == domain.ReviewReplyStatusReplied {
			stats.Answered++
		} else {
			stats.Unanswered++
			bucketUnanswered(&stats.Buckets, now.Sub(r.CreatedAt))
		}

		if r.RepliedAt == nil || r.RepliedAt.Before(r.CreatedAt) {
			continue
		}
		latency := r.RepliedAt.Sub(r.CreatedAt)
		latencies = append(latencies, latency)
		latencySum += latency
		if latency <= target {
			withinTarget++
		}
	}

	stats.MeasuredResponses = len(latencies)
	if stats.MeasuredResponses > 0 {
		stats.MedianResponseHours = roundSLAHours(medianDuration(latencies))
		stats.AverageResponseHours = roundSLAHours(latencySum / time.Duration(stats.MeasuredResponses))
		stats.PercentAnsweredWithinTarget = round2SLA(float64(withinTarget) / float64(stats.MeasuredResponses))
	}
	return stats
}

// bucketUnanswered increments the bucket for an unanswered review of the given
// age (created_at → now). A negative age (a review created in the future by
// clock skew) falls into the youngest bucket.
func bucketUnanswered(b *UnansweredBuckets, age time.Duration) {
	switch {
	case age < slaBucket24h:
		b.Lt24h++
	case age < slaBucket72h:
		b.H24to72++
	default:
		b.Gt72h++
	}
}

// medianDuration returns the median of a non-empty slice. It sorts a copy so
// the caller's slice order is preserved. For an even count it averages the two
// middle values.
func medianDuration(ds []time.Duration) time.Duration {
	sorted := make([]time.Duration, len(ds))
	copy(sorted, ds)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// roundSLAHours converts a duration to hours rounded to two decimals.
func roundSLAHours(d time.Duration) float64 {
	return round2SLA(d.Hours())
}

// round2SLA rounds to two decimal places so hours and rates read cleanly.
func round2SLA(v float64) float64 {
	return math.Round(v*slaCentiScale) / slaCentiScale
}

// SLA loads the business's reviews through the PDn-safe projection and returns
// the aggregate response-SLA metrics as of now. targetHours <= 0 falls back to
// SLADefaultTargetHours. The businessID is the tenant boundary — it comes from
// the RequireBusinessAccess middleware, never a client body — so one business
// can never read another's SLA.
func (s *reviewService) SLA(ctx context.Context, businessID uuid.UUID, targetHours int) (SLAStats, error) {
	reviews, err := s.repo.ListForSLA(ctx, businessID.String())
	if err != nil {
		return SLAStats{}, fmt.Errorf("list reviews for sla: %w", err)
	}
	return computeSLA(reviews, time.Now(), targetHours), nil
}
