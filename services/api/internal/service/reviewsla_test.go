package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// ptrTime is a small helper for the *time.Time fields the SLA math reads.
func ptrTime(t time.Time) *time.Time { return &t }

// slaReview builds a review carrying the fields the SLA read projects, plus a
// name / phone / text so a PDn-leak assertion has something to catch.
func slaReview(status string, createdAt time.Time, repliedAt *time.Time) domain.Review {
	return domain.Review{
		ReplyStatus: status,
		CreatedAt:   createdAt,
		RepliedAt:   repliedAt,
		AuthorName:  "Иван Петров",
		Text:        "Отличное место! Мой телефон +7 900 000 00 00",
		ReplyText:   "Спасибо за отзыв!",
	}
}

// TestComputeSLA_EmptyState proves a business with zero reviews returns honest
// zeros: no division-by-zero, no panic, all buckets zero, default target.
func TestComputeSLA_EmptyState(t *testing.T) {
	got := computeSLA(nil, time.Now(), 0)

	assert.Equal(t, 0, got.Total)
	assert.Equal(t, 0, got.Answered)
	assert.Equal(t, 0, got.Unanswered)
	assert.Equal(t, 0, got.MeasuredResponses)
	assert.Equal(t, 0.0, got.MedianResponseHours)
	assert.Equal(t, 0.0, got.AverageResponseHours)
	assert.Equal(t, 0.0, got.PercentAnsweredWithinTarget)
	assert.Equal(t, SLADefaultTargetHours, got.TargetHours, "a non-positive target falls back to the default")
	assert.Equal(t, UnansweredBuckets{}, got.Buckets)
}

// TestComputeSLA_Buckets proves unanswered reviews land in the right age bucket
// (created_at -> now) and answered reviews never appear in any bucket.
func TestComputeSLA_Buckets(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	reviews := []domain.Review{
		slaReview(domain.ReviewReplyStatusPending, now.Add(-2*time.Hour), nil),   // lt24h
		slaReview(domain.ReviewReplyStatusError, now.Add(-30*time.Hour), nil),    // 24-72h
		slaReview(domain.ReviewReplyStatusPending, now.Add(-100*time.Hour), nil), // gt72h
		slaReview(domain.ReviewReplyStatusPending, now.Add(-73*time.Hour), nil),  // gt72h
		slaReview(domain.ReviewReplyStatusReplied, now.Add(-500*time.Hour), ptrTime(now.Add(-499*time.Hour))),
	}

	got := computeSLA(reviews, now, 24)

	assert.Equal(t, 5, got.Total)
	assert.Equal(t, 1, got.Answered)
	assert.Equal(t, 4, got.Unanswered)
	assert.Equal(t, 1, got.Buckets.Lt24h)
	assert.Equal(t, 1, got.Buckets.H24to72)
	assert.Equal(t, 2, got.Buckets.Gt72h, "an answered review never enters a bucket, even a very old one")
}

// TestComputeSLA_MedianAndAverage_MixedNilExcluded is the behavioral guard for
// the response-time math: reviews without a replied_at are excluded from the
// median / average rather than treated as instant, and the median of an even
// count averages the two middle latencies.
func TestComputeSLA_MedianAndAverage_MixedNilExcluded(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	base := now.Add(-1000 * time.Hour)
	reviews := []domain.Review{
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(2*time.Hour))),  // 2h
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(4*time.Hour))),  // 4h
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(6*time.Hour))),  // 6h
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(20*time.Hour))), // 20h
		slaReview(domain.ReviewReplyStatusReplied, base, nil),                             // answered pre-field: excluded
		slaReview(domain.ReviewReplyStatusPending, now.Add(-3*time.Hour), nil),            // unanswered: excluded
	}

	got := computeSLA(reviews, now, 24)

	assert.Equal(t, 6, got.Total)
	assert.Equal(t, 5, got.Answered)
	assert.Equal(t, 1, got.Unanswered)
	assert.Equal(t, 4, got.MeasuredResponses,
		"only replied reviews carrying a replied_at feed the response-time math")
	assert.InDelta(t, 5.0, got.MedianResponseHours, 1e-9,
		"median of [2,4,6,20]h averages the two middle values -> (4+6)/2 = 5h")
	assert.InDelta(t, 8.0, got.AverageResponseHours, 1e-9,
		"average of [2,4,6,20]h = 8h; the nil-replied_at rows are not counted as instant")
}

// TestComputeSLA_PercentWithinTarget proves the within-target rate is scoped to
// MEASURED responses and respects the supplied target.
func TestComputeSLA_PercentWithinTarget(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	base := now.Add(-1000 * time.Hour)
	reviews := []domain.Review{
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(1*time.Hour))),  // within 24h
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(10*time.Hour))), // within 24h
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(48*time.Hour))), // outside 24h
		slaReview(domain.ReviewReplyStatusReplied, base, nil),                             // excluded from denominator
	}

	got := computeSLA(reviews, now, 24)

	assert.Equal(t, 3, got.MeasuredResponses)
	assert.InDelta(t, 0.67, got.PercentAnsweredWithinTarget, 1e-9,
		"2 of 3 measured responses landed within the 24h target (rounded to 2dp); the un-timestamped reply is excluded")

	tighter := computeSLA(reviews, now, 5)
	assert.InDelta(t, 0.33, tighter.PercentAnsweredWithinTarget, 1e-9,
		"only the 1h reply is within a 5h target (1/3 rounded to 2dp)")
	assert.Equal(t, 5, tighter.TargetHours)
}

// TestComputeSLA_RepliedAtBeforeCreatedIsExcluded proves a clock-skew row
// (replied_at earlier than created_at) is not counted as a negative-latency
// response and does not corrupt the median / average.
func TestComputeSLA_RepliedAtBeforeCreatedIsExcluded(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	base := now.Add(-1000 * time.Hour)
	reviews := []domain.Review{
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(4*time.Hour))),  // 4h, valid
		slaReview(domain.ReviewReplyStatusReplied, base, ptrTime(base.Add(-1*time.Hour))), // skew: excluded
	}

	got := computeSLA(reviews, now, 24)

	assert.Equal(t, 1, got.MeasuredResponses, "the clock-skew row is not a measurable response")
	assert.InDelta(t, 4.0, got.MedianResponseHours, 1e-9)
	assert.InDelta(t, 4.0, got.AverageResponseHours, 1e-9)
}

// TestComputeSLA_AggregateOnly_NoPDn proves the serialized SLA response never
// carries raw review text, author names, or reply text — only numbers. The
// reviews fed in deliberately embed a name and a phone number.
func TestComputeSLA_AggregateOnly_NoPDn(t *testing.T) {
	now := time.Now()
	reviews := []domain.Review{
		slaReview(domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-8*time.Hour))),
		slaReview(domain.ReviewReplyStatusPending, now.Add(-2*time.Hour), nil),
	}

	blob, err := json.Marshal(computeSLA(reviews, now, 24))
	require.NoError(t, err)
	out := string(blob)

	for _, leak := range []string{"Иван", "Петров", "место", "+7 900", "Спасибо", "author", "text", "reply"} {
		assert.Falsef(t, strings.Contains(out, leak),
			"aggregate SLA output must not contain personal or free-text data: found %q in %s", leak, out)
	}
}

// slaStubRepo serves a fixed review set for one business id and records the id
// SLA queried it with, so a test can prove tenant scoping and that the read goes
// through the SLA-projected path. Only ListForSLA is implemented; every other
// method panics via the embedded nil interface so an unexpected call surfaces.
type slaStubRepo struct {
	domain.ReviewRepository
	byBusiness   map[string][]domain.Review
	lastQueried  string
	queriedCount int
}

func (s *slaStubRepo) ListForSLA(_ context.Context, businessID string) ([]domain.Review, error) {
	s.lastQueried = businessID
	s.queriedCount++
	return s.byBusiness[businessID], nil
}

// TestSLA_TenantIsolation proves business A's SLA read is scoped to A's id and
// returns only A's reviews — B's rows never leak into A's aggregate. The stub
// keys on the exact id string, and the service passes businessID.String(), so a
// revert that dropped the business scoping would surface B's data here.
func TestSLA_TenantIsolation(t *testing.T) {
	now := time.Now()
	bizA := uuid.New()
	bizB := uuid.New()

	repo := &slaStubRepo{byBusiness: map[string][]domain.Review{
		bizA.String(): {
			slaReview(domain.ReviewReplyStatusReplied, now.Add(-10*time.Hour), ptrTime(now.Add(-8*time.Hour))),
			slaReview(domain.ReviewReplyStatusPending, now.Add(-2*time.Hour), nil),
		},
		bizB.String(): {
			slaReview(domain.ReviewReplyStatusPending, now.Add(-1*time.Hour), nil),
			slaReview(domain.ReviewReplyStatusPending, now.Add(-1*time.Hour), nil),
			slaReview(domain.ReviewReplyStatusPending, now.Add(-1*time.Hour), nil),
		},
	}}
	svc := &reviewService{repo: repo}

	got, err := svc.SLA(context.Background(), bizA, 24)
	require.NoError(t, err)
	assert.Equal(t, bizA.String(), repo.lastQueried, "SLA must query the caller's own business id")
	assert.Equal(t, 2, got.Total, "business A sees only its own two reviews, never business B's three")
	assert.Equal(t, 1, got.Answered)

	gotB, err := svc.SLA(context.Background(), bizB, 24)
	require.NoError(t, err)
	assert.Equal(t, bizB.String(), repo.lastQueried)
	assert.Equal(t, 3, gotB.Total)
	assert.Equal(t, 0, gotB.Answered)
}

// TestSLA_DefaultTargetHours proves a non-positive target from the handler
// (target_hours absent or malformed -> 0) resolves to the 24h default.
func TestSLA_DefaultTargetHours(t *testing.T) {
	repo := &slaStubRepo{byBusiness: map[string][]domain.Review{}}
	svc := &reviewService{repo: repo}

	got, err := svc.SLA(context.Background(), uuid.New(), 0)
	require.NoError(t, err)
	assert.Equal(t, SLADefaultTargetHours, got.TargetHours)
}
