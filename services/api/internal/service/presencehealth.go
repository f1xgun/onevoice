// Package service — presencehealth.go
//
// PresenceHealth computes a read-only composite 0..100 "is my presence
// healthy?" score for one business by composing over data that already exists:
// the review rating aggregate, the response-SLA computation (reused verbatim
// from reviewsla.go), the answered-coverage share, and the proactive
// platform-sync drift signal. The composite is reputation-weighted:
//
//	composite = 0.50*rating + 0.25*sla + 0.15*coverage + 0.10*sync
//
// Every sub-score is normalized to 0..100. The score is aggregate-only: it
// reads no author names, review text, or reply text — the rating read uses the
// PDn-safe ratingStatsProjection, the same discipline as the SLA read.
//
// The scoring math (computePresenceHealth) is a pure function over slices so it
// is unit-testable without a database; PresenceHealthService is the thin
// repository-backed wrapper that feeds it.

package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Reputation weights. They sum to 1.0 and are LOCKED product decisions: the
// composite is reputation-weighted so a business's star rating dominates, then
// how fast it answers (SLA), then how much it answers (coverage), then whether
// the connected platforms are in sync. Changing any constant changes the
// composite for every business — the exact-weight test fails on a revert.
const (
	presenceRatingWeight   = 0.50
	presenceSLAWeight      = 0.25
	presenceCoverageWeight = 0.15
	presenceSyncWeight     = 0.10
)

// syncScoreSynced / syncScoreDrifting map a per-channel drift flag to a 0..100
// sync sub-score. A channel in sync scores full; a drifting channel scores low
// but not zero (drift is recoverable, not a total outage). The business's sync
// sub-score is the mean across its channels.
const (
	syncScoreSynced   = 100
	syncScoreDrifting = 40
)

// presenceMaxStars normalizes an average star rating in [1,5] to 0..100 via
// (avg/maxStars)*100.
const presenceMaxStars = 5

// presencePercentScale converts a [0,1] rate to a 0..100 sub-score.
const presencePercentScale = 100

// presenceScoreMax is the ceiling every sub-score and the composite are clamped
// to, so rounding can never push a value above 100.
const presenceScoreMax = 100

// PresenceArea names the composite dimension a recommendation targets. It is a
// stable machine key the frontend maps to a localized label; the human message
// key is carried separately in PresenceRecommendation.
type PresenceArea string

const (
	PresenceAreaRating   PresenceArea = "rating"
	PresenceAreaSLA      PresenceArea = "sla"
	PresenceAreaCoverage PresenceArea = "coverage"
	PresenceAreaSync     PresenceArea = "sync"
)

// PresenceWeights is the weight set the composite was computed under. It is
// echoed in the response so a client can show the breakdown and so the
// renormalized (sync-dropped) weights are transparent rather than implicit.
type PresenceWeights struct {
	Rating   float64
	SLA      float64
	Coverage float64
	Sync     float64
}

// PresenceSubScores holds each 0..100 dimension. Sync is nullable: nil means
// the business has no readable sync signal (no connected channels / never
// reconciled), so the sync dimension was dropped and the other three weights
// renormalized. The rating/sla/coverage pointers are nil only in the empty
// state (no reviews at all), which also nulls the composite.
type PresenceSubScores struct {
	Rating   *int
	SLA      *int
	Coverage *int
	Sync     *int
}

// PresenceRecommendation is the single next-action nudge: the weakest sub-score
// area, a localized message key the frontend resolves, and the potential
// composite points recovering that area to 100 would add
// (round(weight_of_area * (100 - subScore))).
type PresenceRecommendation struct {
	Area          PresenceArea
	MessageKey    string
	PotentialGain int
}

// PresenceHealthScore is the composed result. Composite is nil in the empty
// state (no reviews). TrendDelta is filled by the service from the prior-week
// snapshot, not by the pure scorer, so it is not on this struct.
type PresenceHealthScore struct {
	Composite      *int
	SubScores      PresenceSubScores
	Weights        PresenceWeights
	Recommendation *PresenceRecommendation
}

// presenceRecommendationKeys maps each area to the i18n message key the
// frontend resolves to a localized fix suggestion. Backend supplies only the
// key; ru/en copy lives in the frontend locale files (deferred to the FE owner).
var presenceRecommendationKeys = map[PresenceArea]string{
	PresenceAreaRating:   "presenceHealth.recommendation.rating",
	PresenceAreaSLA:      "presenceHealth.recommendation.sla",
	PresenceAreaCoverage: "presenceHealth.recommendation.coverage",
	PresenceAreaSync:     "presenceHealth.recommendation.sync",
}

// clampScore bounds a raw score into [0, presenceScoreMax].
func clampScore(v int) int {
	if v < 0 {
		return 0
	}
	if v > presenceScoreMax {
		return presenceScoreMax
	}
	return v
}

// intPtr returns a pointer to v; used to fill the nullable sub-score / composite
// fields.
func intPtr(v int) *int { return &v }

// computeRatingScore normalizes the average star rating over reviews carrying a
// valid rating in [1,5]. It returns ok=false when no review has a valid rating,
// so the caller can null the dimension rather than report a misleading 0.
func computeRatingScore(reviews []domain.Review) (score int, ok bool) {
	var sum, count int
	for i := range reviews {
		r := reviews[i].Rating
		if r >= 1 && r <= presenceMaxStars {
			sum += r
			count++
		}
	}
	if count == 0 {
		return 0, false
	}
	avg := float64(sum) / float64(count)
	return clampScore(int(math.Round(avg / presenceMaxStars * presencePercentScale))), true
}

// computeCoverageScore is the answered share (reply_status == "replied") over
// all reviews, ×100. It returns ok=false when there are no reviews at all.
func computeCoverageScore(reviews []domain.Review) (score int, ok bool) {
	if len(reviews) == 0 {
		return 0, false
	}
	answered := 0
	for i := range reviews {
		if reviews[i].ReplyStatus == domain.ReviewReplyStatusReplied {
			answered++
		}
	}
	return clampScore(int(math.Round(float64(answered) / float64(len(reviews)) * presencePercentScale))), true
}

// computeSLAScore reuses computeSLA (from reviewsla.go, the #462 math) and reads
// its PercentAnsweredWithinTarget [0,1] as the 0..100 SLA sub-score. It returns
// ok=false when no reply latency could be measured (MeasuredResponses == 0), so
// a business that has never answered a timestamped review nulls the dimension
// rather than reporting a misleading 0.
func computeSLAScore(reviews []domain.Review, now time.Time, targetHours int) (score int, ok bool) {
	stats := computeSLA(reviews, now, targetHours)
	if stats.MeasuredResponses == 0 {
		return 0, false
	}
	return clampScore(int(math.Round(stats.PercentAnsweredWithinTarget * presencePercentScale))), true
}

// computeSyncScore maps the business's sync_state rows to a 0..100 mean: a
// non-drifting channel scores syncScoreSynced, a drifting one scores
// syncScoreDrifting. It returns ok=false when the business has ZERO sync_state
// rows — the sync signal is undefined for that business, so the dimension is
// dropped and the other weights renormalize over 0.90.
func computeSyncScore(states []domain.SyncState) (score int, ok bool) {
	if len(states) == 0 {
		return 0, false
	}
	sum := 0
	for i := range states {
		if states[i].DriftDetected {
			sum += syncScoreDrifting
		} else {
			sum += syncScoreSynced
		}
	}
	return clampScore(int(math.Round(float64(sum) / float64(len(states))))), true
}

// presenceDimension is one weighted, present-or-absent sub-score used while
// composing the composite and picking the weakest area.
type presenceDimension struct {
	area   PresenceArea
	score  int
	weight float64
	ok     bool
}

// computePresenceHealth is the pure scorer: it reduces a business's reviews and
// sync-state rows to the composite score, sub-scores, the weight set actually
// used, and the single weakest-area recommendation.
//
// Empty state (no reviews at all → rating, sla, and coverage all absent):
// Composite nil, every review-derived sub-score nil. When sync is also absent
// the whole score is empty (composite nil, no recommendation).
//
// Sync-dropped state (reviews present, zero sync_state rows): Sync nil, and the
// rating/sla/coverage weights renormalize over 0.90 (each divided by 0.90) so
// the composite still sums to 100.
func computePresenceHealth(reviews []domain.Review, states []domain.SyncState, now time.Time, targetHours int) PresenceHealthScore {
	ratingScore, ratingOK := computeRatingScore(reviews)
	slaScore, slaOK := computeSLAScore(reviews, now, targetHours)
	coverageScore, coverageOK := computeCoverageScore(reviews)
	syncScore, syncOK := computeSyncScore(states)

	sub := PresenceSubScores{}
	if ratingOK {
		sub.Rating = intPtr(ratingScore)
	}
	if slaOK {
		sub.SLA = intPtr(slaScore)
	}
	if coverageOK {
		sub.Coverage = intPtr(coverageScore)
	}
	if syncOK {
		sub.Sync = intPtr(syncScore)
	}

	dims := []presenceDimension{
		{area: PresenceAreaRating, score: ratingScore, weight: presenceRatingWeight, ok: ratingOK},
		{area: PresenceAreaSLA, score: slaScore, weight: presenceSLAWeight, ok: slaOK},
		{area: PresenceAreaCoverage, score: coverageScore, weight: presenceCoverageWeight, ok: coverageOK},
		{area: PresenceAreaSync, score: syncScore, weight: presenceSyncWeight, ok: syncOK},
	}

	weights, activeDenominator := presenceWeightsFor(ratingOK, slaOK, coverageOK, syncOK)

	score := PresenceHealthScore{SubScores: sub, Weights: weights}

	if activeDenominator == 0 {
		return score
	}

	var weightedSum float64
	for _, d := range dims {
		if !d.ok {
			continue
		}
		weightedSum += (d.weight / activeDenominator) * float64(d.score)
	}
	composite := clampScore(int(math.Round(weightedSum)))
	score.Composite = intPtr(composite)
	score.Recommendation = weakestRecommendation(dims, weights)
	return score
}

// presenceWeightsFor returns the weight set the composite is actually computed
// under and the denominator the active dimensions are normalized over.
//
// The denominator is the SUM of the weights of the PRESENT dimensions, so the
// composite is a weighted average over exactly the dimensions we can measure —
// this is what implements "drop sync and renormalize the other three over 0.90"
// and, more generally, renormalizing over whatever subset is present. The
// echoed Weights are the same renormalized fractions, so the response is
// transparent about the mix used.
func presenceWeightsFor(ratingOK, slaOK, coverageOK, syncOK bool) (weights PresenceWeights, denominator float64) {
	denom := 0.0
	if ratingOK {
		denom += presenceRatingWeight
	}
	if slaOK {
		denom += presenceSLAWeight
	}
	if coverageOK {
		denom += presenceCoverageWeight
	}
	if syncOK {
		denom += presenceSyncWeight
	}
	if denom == 0 {
		return PresenceWeights{}, 0
	}
	w := PresenceWeights{}
	if ratingOK {
		w.Rating = presenceRatingWeight / denom
	}
	if slaOK {
		w.SLA = presenceSLAWeight / denom
	}
	if coverageOK {
		w.Coverage = presenceCoverageWeight / denom
	}
	if syncOK {
		w.Sync = presenceSyncWeight / denom
	}
	return w, denom
}

// weakestRecommendation picks the lowest-scoring PRESENT dimension and returns
// the next-action nudge for it. potentialGain is how many composite points
// recovering that area to 100 would add under the weight set actually used:
// round(renormalizedWeight * (100 - subScore)). Ties break by the natural
// dimension order (rating, sla, coverage, sync). Returns nil when no dimension
// is present (empty state).
func weakestRecommendation(dims []presenceDimension, weights PresenceWeights) *PresenceRecommendation {
	var weak presenceDimension
	found := false
	for _, d := range dims {
		if !d.ok {
			continue
		}
		if !found || d.score < weak.score {
			weak = d
			found = true
		}
	}
	if !found {
		return nil
	}
	weight := weightForArea(weights, weak.area)
	gain := int(math.Round(weight * float64(presenceScoreMax-weak.score)))
	return &PresenceRecommendation{
		Area:          weak.area,
		MessageKey:    presenceRecommendationKeys[weak.area],
		PotentialGain: gain,
	}
}

// weightForArea reads the renormalized weight of one area out of the echoed set.
func weightForArea(w PresenceWeights, area PresenceArea) float64 {
	switch area {
	case PresenceAreaRating:
		return w.Rating
	case PresenceAreaSLA:
		return w.SLA
	case PresenceAreaCoverage:
		return w.Coverage
	case PresenceAreaSync:
		return w.Sync
	default:
		return 0
	}
}

// PresenceHealthReviewReader loads a business's reviews through the PDn-safe
// rating-stats projection. *reviewRepository (via domain.ReviewRepository)
// satisfies it.
type PresenceHealthReviewReader interface {
	ListForRatingStats(ctx context.Context, businessID string) ([]domain.Review, error)
}

// PresenceHealthSyncReader loads a business's sync_state rows for the sync
// sub-score. domain.SyncStateRepository satisfies it.
type PresenceHealthSyncReader interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.SyncState, error)
}

// PresenceHealthService composes the presence-health score for one business
// from the review + sync reads. It owns no HTTP or persistence concerns — the
// handler maps its result to the wire shape and (optionally) stamps the weekly
// snapshot.
type PresenceHealthService struct {
	reviews PresenceHealthReviewReader
	sync    PresenceHealthSyncReader
	now     func() time.Time
}

// NewPresenceHealthService constructs the scorer over its two read
// dependencies. now defaults to time.Now.
func NewPresenceHealthService(reviews PresenceHealthReviewReader, sync PresenceHealthSyncReader) *PresenceHealthService {
	return &PresenceHealthService{reviews: reviews, sync: sync, now: time.Now}
}

// Score composes the presence-health score for businessID as of now. targetHours
// <= 0 falls back to the SLA default. businessID is the tenant boundary — it
// comes from the RequireBusinessAccess middleware, never a client body — so one
// business can never read another's score. The reads are aggregate-only and
// PDn-free by construction.
func (s *PresenceHealthService) Score(ctx context.Context, businessID uuid.UUID, targetHours int) (PresenceHealthScore, error) {
	reviews, err := s.reviews.ListForRatingStats(ctx, businessID.String())
	if err != nil {
		return PresenceHealthScore{}, fmt.Errorf("presence health: list reviews: %w", err)
	}
	states, err := s.sync.ListByBusinessID(ctx, businessID)
	if err != nil {
		return PresenceHealthScore{}, fmt.Errorf("presence health: list sync state: %w", err)
	}
	return computePresenceHealth(reviews, states, s.now(), targetHours), nil
}
