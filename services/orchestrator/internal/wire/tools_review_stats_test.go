package wire

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/reviewstats"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// fakeFetcher returns a per-business review set and records the business id it
// was queried with, so a test can prove the executor scopes to trusted context.
type fakeFetcher struct {
	byBusiness map[string][]domain.Review
	lastID     string
	err        error
}

func (f *fakeFetcher) FetchForBusiness(_ context.Context, businessID string) ([]domain.Review, error) {
	f.lastID = businessID
	if f.err != nil {
		return nil, f.err
	}
	return f.byBusiness[businessID], nil
}

func statsResult(t *testing.T, out interface{}) reviewstats.Stats {
	t.Helper()
	s, ok := out.(reviewstats.Stats)
	require.True(t, ok, "result must be a reviewstats.Stats so aggregates surface to the LLM")
	return s
}

// TestRegisterReviewStatsTool_Registered is the fail-on-revert registration
// guard: a non-nil fetcher registers get_review_stats with an Auto floor, and it
// must surface in both Available(nil) and an EXPLICIT whitelist (Auto exempts
// the whitelist) — exactly like the other bare-name internal tool.
func TestRegisterReviewStatsTool_Registered(t *testing.T) {
	reg := toolregistry.NewRegistry()
	RegisterReviewStatsTool(reg, &fakeFetcher{})

	require.True(t, reg.Has(tools.GetReviewStats), "get_review_stats must be registered")
	assert.Equal(t, domain.ToolFloorAuto, reg.Floor(tools.GetReviewStats))

	assert.Contains(t, defNames(reg.Available(nil)), tools.GetReviewStats,
		"bare-name tool must be available with no active integrations")

	wl := reg.AvailableForWhitelist(context.Background(), nil, domain.WhitelistModeExplicit, nil)
	assert.Contains(t, defNames(wl), tools.GetReviewStats,
		"Auto-floor tool must pass an EXPLICIT whitelist even when not listed")
}

// TestRegisterReviewStatsTool_NilFetcher proves a missing data source registers
// no tool rather than a handler that would fail every call.
func TestRegisterReviewStatsTool_NilFetcher(t *testing.T) {
	reg := toolregistry.NewRegistry()
	RegisterReviewStatsTool(reg, nil)
	assert.False(t, reg.Has(tools.GetReviewStats), "nil fetcher must not register get_review_stats")
}

// TestReviewStatsExecutor_ScopedToBusinessContext proves the executor aggregates
// the CURRENT business's reviews, taken from trusted turn context.
func TestReviewStatsExecutor_ScopedToBusinessContext(t *testing.T) {
	bizA := uuid.New().String()
	now := time.Now()
	fetcher := &fakeFetcher{byBusiness: map[string][]domain.Review{
		bizA: {
			{Rating: 5, ReplyStatus: domain.ReviewReplyStatusReplied, CreatedAt: now},
			{Rating: 3, ReplyStatus: domain.ReviewReplyStatusPending, CreatedAt: now},
		},
	}}
	exec := newReviewStatsExecutor(fetcher)

	ctx := a2a.WithBusinessID(context.Background(), bizA)
	out, err := exec(ctx, map[string]interface{}{})
	require.NoError(t, err)

	assert.Equal(t, bizA, fetcher.lastID, "fetch must be scoped to the ctx business id")
	stats := statsResult(t, out)
	assert.Equal(t, 2, stats.Total)
	assert.Equal(t, 1, stats.Answered)
}

// TestReviewStatsExecutor_TenantIsolation proves business A cannot read business
// B's stats even if the model passes B's id as an argument: the executor ignores
// any id in args and uses only the trusted context id. A's context over B's
// dataset returns A's (empty) stats, and the fetch is scoped to A.
func TestReviewStatsExecutor_TenantIsolation(t *testing.T) {
	bizA := uuid.New().String()
	bizB := uuid.New().String()
	now := time.Now()
	fetcher := &fakeFetcher{byBusiness: map[string][]domain.Review{
		bizB: {
			{Rating: 5, ReplyStatus: domain.ReviewReplyStatusReplied, CreatedAt: now},
			{Rating: 4, ReplyStatus: domain.ReviewReplyStatusReplied, CreatedAt: now},
		},
	}}
	exec := newReviewStatsExecutor(fetcher)

	ctx := a2a.WithBusinessID(context.Background(), bizA)
	out, err := exec(ctx, map[string]interface{}{
		"business_id": bizB,
		"businessId":  bizB,
	})
	require.NoError(t, err)

	assert.Equal(t, bizA, fetcher.lastID,
		"a business_id in args must be ignored; only the trusted context id is used")
	assert.NotEqual(t, bizB, fetcher.lastID, "must never read another organization's id from args")
	stats := statsResult(t, out)
	assert.Equal(t, 0, stats.Total, "business A sees its own empty stats, not business B's two reviews")
}

// TestReviewStatsExecutor_MissingBusinessContext refuses to fetch when no
// business id is present, so a missing context can never fan out to a
// cross-tenant read.
func TestReviewStatsExecutor_MissingBusinessContext(t *testing.T) {
	fetcher := &fakeFetcher{}
	exec := newReviewStatsExecutor(fetcher)

	_, err := exec(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	assert.Empty(t, fetcher.lastID, "must not query without a business id")
}

// TestReviewStatsExecutor_EmptyCollection proves a business with zero reviews
// returns clean zeros through the executor path, never an error.
func TestReviewStatsExecutor_EmptyCollection(t *testing.T) {
	exec := newReviewStatsExecutor(&fakeFetcher{byBusiness: map[string][]domain.Review{}})

	ctx := a2a.WithBusinessID(context.Background(), uuid.New().String())
	out, err := exec(ctx, map[string]interface{}{})
	require.NoError(t, err)

	stats := statsResult(t, out)
	assert.Equal(t, 0, stats.Total)
	assert.Equal(t, 0.0, stats.ReplyRate)
	assert.Equal(t, 0.0, stats.AverageRating)
}

// TestReviewStatsExecutor_FetchError maps a data-layer failure to a clean tool
// error without leaking the underlying detail.
func TestReviewStatsExecutor_FetchError(t *testing.T) {
	exec := newReviewStatsExecutor(&fakeFetcher{err: errors.New("mongo: connection refused at 10.0.0.5")})

	ctx := a2a.WithBusinessID(context.Background(), uuid.New().String())
	_, err := exec(ctx, map[string]interface{}{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "10.0.0.5", "raw infra detail must not leak to the tool result")
}

// TestReviewStatsExecutor_ClampsRecentDays proves an out-of-bounds recent_days
// argument is clamped to the advertised maximum rather than widening the window
// arbitrarily.
func TestReviewStatsExecutor_ClampsRecentDays(t *testing.T) {
	bizA := uuid.New().String()
	fetcher := &fakeFetcher{byBusiness: map[string][]domain.Review{bizA: nil}}
	exec := newReviewStatsExecutor(fetcher)

	ctx := a2a.WithBusinessID(context.Background(), bizA)
	out, err := exec(ctx, map[string]interface{}{"recent_days": float64(100000)})
	require.NoError(t, err)

	stats := statsResult(t, out)
	assert.Equal(t, reviewStatsMaxDays, stats.RecentDays, "recent_days must clamp to the max window")
}
