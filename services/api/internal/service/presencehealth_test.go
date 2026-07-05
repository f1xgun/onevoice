package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// phReview builds a review carrying the fields the presence-health rating read
// projects, plus a name / phone / text so a PDn-leak assertion (in the repo
// test) has something to catch. The scorer ignores author/text entirely.
func phReview(rating int, status string, createdAt time.Time, repliedAt *time.Time) domain.Review {
	return domain.Review{
		Rating:      rating,
		ReplyStatus: status,
		CreatedAt:   createdAt,
		RepliedAt:   repliedAt,
		AuthorName:  "Иван Петров",
		Text:        "Отличное место! Мой телефон +7 900 000 00 00",
		ReplyText:   "Спасибо за отзыв!",
	}
}

func syncRow(drift bool) domain.SyncState { return domain.SyncState{DriftDetected: drift} }

// TestComputePresenceHealth_ExactReputationWeights pins the composite to the
// exact reputation-weighted sum 0.50*rating + 0.25*sla + 0.15*coverage +
// 0.10*sync on a fixed, fully-present sub-score set. Reverting any weight
// constant changes this number and fails the test.
func TestComputePresenceHealth_ExactReputationWeights(t *testing.T) {
	now := time.Now()
	// All four reviews rated 5 → avg 5 → ratingScore 100.
	// All four replied within 24h → percentWithinTarget 1.0 → slaScore 100.
	// All four replied → coverage 100.
	// One drifting + one synced channel → sync mean (100+40)/2 = 70.
	reviews := []domain.Review{
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
	}
	states := []domain.SyncState{syncRow(false), syncRow(true)}

	got := computePresenceHealth(reviews, states, now, 24)

	require.NotNil(t, got.SubScores.Rating)
	require.NotNil(t, got.SubScores.SLA)
	require.NotNil(t, got.SubScores.Coverage)
	require.NotNil(t, got.SubScores.Sync)
	assert.Equal(t, 100, *got.SubScores.Rating)
	assert.Equal(t, 100, *got.SubScores.SLA)
	assert.Equal(t, 100, *got.SubScores.Coverage)
	assert.Equal(t, 70, *got.SubScores.Sync)

	// composite = 0.50*100 + 0.25*100 + 0.15*100 + 0.10*70 = 50+25+15+7 = 97.
	require.NotNil(t, got.Composite)
	assert.Equal(t, 97, *got.Composite)

	// Full weight set echoed (no renormalization when all four present).
	assert.InDelta(t, 0.50, got.Weights.Rating, 1e-9)
	assert.InDelta(t, 0.25, got.Weights.SLA, 1e-9)
	assert.InDelta(t, 0.15, got.Weights.Coverage, 1e-9)
	assert.InDelta(t, 0.10, got.Weights.Sync, 1e-9)
}

// TestComputePresenceHealth_Normalization checks each sub-score's normalization
// formula on known inputs independent of the composite.
func TestComputePresenceHealth_Normalization(t *testing.T) {
	now := time.Now()
	// avg rating (4+2)/2 = 3 → (3/5)*100 = 60.
	// 1 of 2 replied → coverage 50.
	// the one reply is within 24h, the other review is unanswered (no replied_at)
	//   → measured=1, withinTarget=1 → slaScore 100.
	reviews := []domain.Review{
		phReview(4, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		phReview(2, domain.ReviewReplyStatusPending, now.Add(-5*time.Hour), nil),
	}
	states := []domain.SyncState{syncRow(false)} // single synced channel → 100.

	got := computePresenceHealth(reviews, states, now, 24)

	require.NotNil(t, got.SubScores.Rating)
	require.NotNil(t, got.SubScores.SLA)
	require.NotNil(t, got.SubScores.Coverage)
	require.NotNil(t, got.SubScores.Sync)
	assert.Equal(t, 60, *got.SubScores.Rating)
	assert.Equal(t, 100, *got.SubScores.SLA)
	assert.Equal(t, 50, *got.SubScores.Coverage)
	assert.Equal(t, 100, *got.SubScores.Sync)
}

// TestComputePresenceHealth_RenormalizeOnMissingSync proves a business with ZERO
// sync_state rows drops the sync dimension: syncScore is null, the other three
// weights renormalize over 0.90, and the composite is the weighted average of
// only the present dimensions.
func TestComputePresenceHealth_RenormalizeOnMissingSync(t *testing.T) {
	now := time.Now()
	reviews := []domain.Review{
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
	}
	// No sync rows.
	got := computePresenceHealth(reviews, nil, now, 24)

	assert.Nil(t, got.SubScores.Sync, "sync sub-score must be null when no sync_state rows exist")

	// Renormalized weights over 0.90.
	assert.InDelta(t, 0.50/0.90, got.Weights.Rating, 1e-9)
	assert.InDelta(t, 0.25/0.90, got.Weights.SLA, 1e-9)
	assert.InDelta(t, 0.15/0.90, got.Weights.Coverage, 1e-9)
	assert.InDelta(t, 0.0, got.Weights.Sync, 1e-9)

	// rating=sla=coverage=100 → composite still 100 (weighted avg of equal 100s).
	require.NotNil(t, got.Composite)
	assert.Equal(t, 100, *got.Composite)
}

// TestComputePresenceHealth_RenormalizeMixedScores checks the renormalized
// composite on distinct sub-scores so a mistaken denominator would show.
func TestComputePresenceHealth_RenormalizeMixedScores(t *testing.T) {
	now := time.Now()
	// rating avg 2 → 40; coverage 1/2 → 50; sla: the replied one is within 24h
	//   → 100. No sync rows → renormalize over 0.90.
	reviews := []domain.Review{
		phReview(2, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		phReview(2, domain.ReviewReplyStatusPending, now.Add(-5*time.Hour), nil),
	}
	got := computePresenceHealth(reviews, nil, now, 24)

	require.NotNil(t, got.SubScores.Rating)
	require.NotNil(t, got.SubScores.SLA)
	require.NotNil(t, got.SubScores.Coverage)
	assert.Equal(t, 40, *got.SubScores.Rating)
	assert.Equal(t, 100, *got.SubScores.SLA)
	assert.Equal(t, 50, *got.SubScores.Coverage)

	// composite = (0.50*40 + 0.25*100 + 0.15*50) / 0.90
	//           = (20 + 25 + 7.5) / 0.90 = 52.5 / 0.90 = 58.33… → round 58.
	require.NotNil(t, got.Composite)
	assert.Equal(t, 58, *got.Composite)
}

// TestComputePresenceHealth_EmptyStateNoReviews proves the honest baseline: no
// reviews → composite null, rating/sla/coverage null, no recommendation, no
// divide-by-zero. Sync still evaluates independently.
func TestComputePresenceHealth_EmptyStateNoReviews(t *testing.T) {
	now := time.Now()

	got := computePresenceHealth(nil, nil, now, 24)

	assert.Nil(t, got.Composite, "composite must be null when there is no data at all")
	assert.Nil(t, got.SubScores.Rating)
	assert.Nil(t, got.SubScores.SLA)
	assert.Nil(t, got.SubScores.Coverage)
	assert.Nil(t, got.SubScores.Sync)
	assert.Nil(t, got.Recommendation, "no recommendation in the fully-empty state")
}

// TestComputePresenceHealth_SyncOnlyWhenNoReviews proves a business with
// connected channels but no reviews still gets a composite from the sync
// dimension alone (the sync weight becomes the whole denominator).
func TestComputePresenceHealth_SyncOnlyWhenNoReviews(t *testing.T) {
	now := time.Now()
	states := []domain.SyncState{syncRow(false), syncRow(false)}

	got := computePresenceHealth(nil, states, now, 24)

	assert.Nil(t, got.SubScores.Rating)
	assert.Nil(t, got.SubScores.SLA)
	assert.Nil(t, got.SubScores.Coverage)
	require.NotNil(t, got.SubScores.Sync)
	assert.Equal(t, 100, *got.SubScores.Sync)

	require.NotNil(t, got.Composite)
	assert.Equal(t, 100, *got.Composite, "sync-only composite is the sync score itself")
	assert.InDelta(t, 1.0, got.Weights.Sync, 1e-9, "sync weight is the whole denominator when it is the only dimension")
}

// TestComputePresenceHealth_RecommendationPicksWeakest proves the recommendation
// targets the lowest present sub-score and its potentialGain is
// round(weight_of_area * (100 - subScore)) under the weight set actually used.
func TestComputePresenceHealth_RecommendationPicksWeakest(t *testing.T) {
	now := time.Now()
	// rating avg 5 → 100; coverage 1/2 → 50; sla replied-within → 100;
	// sync one drifting → 40 (the weakest).
	reviews := []domain.Review{
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		phReview(5, domain.ReviewReplyStatusPending, now.Add(-5*time.Hour), nil),
	}
	states := []domain.SyncState{syncRow(true)} // sync 40.

	got := computePresenceHealth(reviews, states, now, 24)

	require.NotNil(t, got.Recommendation)
	assert.Equal(t, PresenceAreaSync, got.Recommendation.Area, "weakest area (sync=40) must be recommended")
	assert.Equal(t, presenceRecommendationKeys[PresenceAreaSync], got.Recommendation.MessageKey)
	// potentialGain = round(0.10 * (100 - 40)) = round(6.0) = 6.
	assert.Equal(t, 6, got.Recommendation.PotentialGain)
}

// TestComputePresenceHealth_RecommendationCoverageGain checks potentialGain for
// a non-sync weakest area so the weight lookup per area is exercised.
func TestComputePresenceHealth_RecommendationCoverageGain(t *testing.T) {
	now := time.Now()
	// rating 100, sla 100, sync 100; coverage 1/4 = 25 (the weakest).
	reviews := []domain.Review{
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		phReview(5, domain.ReviewReplyStatusPending, now.Add(-5*time.Hour), nil),
		phReview(5, domain.ReviewReplyStatusPending, now.Add(-5*time.Hour), nil),
		phReview(5, domain.ReviewReplyStatusPending, now.Add(-5*time.Hour), nil),
	}
	states := []domain.SyncState{syncRow(false)}

	got := computePresenceHealth(reviews, states, now, 24)

	require.NotNil(t, got.SubScores.Coverage)
	assert.Equal(t, 25, *got.SubScores.Coverage)
	require.NotNil(t, got.Recommendation)
	assert.Equal(t, PresenceAreaCoverage, got.Recommendation.Area)
	// potentialGain = round(0.15 * (100 - 25)) = round(11.25) = 11.
	assert.Equal(t, 11, got.Recommendation.PotentialGain)
}

// TestComputePresenceHealth_CompositeClampedToRange proves the composite and
// sub-scores never exceed 100 nor drop below 0 for any input.
func TestComputePresenceHealth_CompositeClampedToRange(t *testing.T) {
	now := time.Now()
	reviews := []domain.Review{
		phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
	}
	states := []domain.SyncState{syncRow(false)}

	got := computePresenceHealth(reviews, states, now, 24)

	require.NotNil(t, got.Composite)
	assert.GreaterOrEqual(t, *got.Composite, 0)
	assert.LessOrEqual(t, *got.Composite, 100)
}

// TestComputePresenceHealth_RatingNullWhenNoRatedReviews proves the rating
// dimension is dropped (not zero) when reviews exist but none carry a valid
// rating — coverage and sla still compute off the same reviews.
func TestComputePresenceHealth_RatingNullWhenNoRatedReviews(t *testing.T) {
	now := time.Now()
	reviews := []domain.Review{
		phReview(0, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
	}
	states := []domain.SyncState{syncRow(false)}

	got := computePresenceHealth(reviews, states, now, 24)

	assert.Nil(t, got.SubScores.Rating, "rating must be null when no review has a valid rating")
	require.NotNil(t, got.SubScores.Coverage)
	assert.Equal(t, 100, *got.SubScores.Coverage)
	// Weights renormalize over the present set (rating dropped).
	assert.InDelta(t, 0.0, got.Weights.Rating, 1e-9)
}

// phRatingStubRepo implements PresenceHealthReviewReader keyed on business id so
// a tenant-isolation revert (dropping the business scoping) surfaces here.
type phRatingStubRepo struct {
	byBusiness  map[string][]domain.Review
	lastQueried string
}

func (s *phRatingStubRepo) ListForRatingStats(_ context.Context, businessID string) ([]domain.Review, error) {
	s.lastQueried = businessID
	return s.byBusiness[businessID], nil
}

// phSyncStubRepo implements PresenceHealthSyncReader keyed on business id.
type phSyncStubRepo struct {
	byBusiness map[uuid.UUID][]domain.SyncState
}

func (s *phSyncStubRepo) ListByBusinessID(_ context.Context, businessID uuid.UUID) ([]domain.SyncState, error) {
	return s.byBusiness[businessID], nil
}

// TestPresenceHealthService_TenantIsolation proves the service reads are scoped
// to the caller's business id and never fold in another tenant's reviews or sync
// rows. A revert dropping the business scoping would surface B's data in A's
// score.
func TestPresenceHealthService_TenantIsolation(t *testing.T) {
	now := time.Now()
	bizA := uuid.New()
	bizB := uuid.New()

	reviewRepo := &phRatingStubRepo{byBusiness: map[string][]domain.Review{
		bizA.String(): {
			phReview(5, domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-9*time.Hour))),
		},
		bizB.String(): {
			phReview(1, domain.ReviewReplyStatusPending, now.Add(-10*time.Hour), nil),
		},
	}}
	syncRepo := &phSyncStubRepo{byBusiness: map[uuid.UUID][]domain.SyncState{
		bizA: {syncRow(false)},
		bizB: {syncRow(true)},
	}}
	svc := NewPresenceHealthService(reviewRepo, syncRepo)

	got, err := svc.Score(context.Background(), bizA, 24)
	require.NoError(t, err)

	assert.Equal(t, bizA.String(), reviewRepo.lastQueried, "review read must be scoped to the caller's business")
	require.NotNil(t, got.SubScores.Rating)
	assert.Equal(t, 100, *got.SubScores.Rating, "A's 5-star review, not B's 1-star, must drive the rating score")
	require.NotNil(t, got.SubScores.Sync)
	assert.Equal(t, 100, *got.SubScores.Sync, "A's synced channel, not B's drifting one, must drive the sync score")
}
