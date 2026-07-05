package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// stubPresenceScorer implements PresenceHealthScorer for handler tests.
type stubPresenceScorer struct {
	scoreFn func(ctx context.Context, businessID uuid.UUID, targetHours int) (service.PresenceHealthScore, error)
}

func (s *stubPresenceScorer) Score(ctx context.Context, businessID uuid.UUID, targetHours int) (service.PresenceHealthScore, error) {
	return s.scoreFn(ctx, businessID, targetHours)
}

// stubSnapshotStore implements PresenceHealthSnapshotStore for handler tests.
type stubSnapshotStore struct {
	prior      *domain.PresenceHealthSnapshot
	priorErr   error
	upserted   []domain.PresenceHealthSnapshot
	priorWeek  string
	priorBizID uuid.UUID
}

func (s *stubSnapshotStore) GetMostRecentPrior(_ context.Context, businessID uuid.UUID, currentWeek string) (*domain.PresenceHealthSnapshot, error) {
	s.priorBizID = businessID
	s.priorWeek = currentWeek
	return s.prior, s.priorErr
}

func (s *stubSnapshotStore) Upsert(_ context.Context, snap domain.PresenceHealthSnapshot) error {
	s.upserted = append(s.upserted, snap)
	return nil
}

func ptrInt(v int) *int { return &v }

func fullScore(composite int) service.PresenceHealthScore {
	return service.PresenceHealthScore{
		Composite: ptrInt(composite),
		SubScores: service.PresenceSubScores{
			Rating:   ptrInt(90),
			SLA:      ptrInt(80),
			Coverage: ptrInt(70),
			Sync:     ptrInt(60),
		},
		Weights: service.PresenceWeights{Rating: 0.50, SLA: 0.25, Coverage: 0.15, Sync: 0.10},
		Recommendation: &service.PresenceRecommendation{
			Area:          service.PresenceAreaSync,
			MessageKey:    "presenceHealth.recommendation.sync",
			PotentialGain: 4,
		},
	}
}

// TestGetPresenceHealth_Success proves the handler maps the service score to the
// wire shape, forwards target_hours, and lazily upserts the current-week
// snapshot.
func TestGetPresenceHealth_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	scorer := &stubPresenceScorer{
		scoreFn: func(_ context.Context, gotBiz uuid.UUID, targetHours int) (service.PresenceHealthScore, error) {
			assert.Equal(t, businessID, gotBiz, "score must be scoped to the caller's business")
			assert.Equal(t, 48, targetHours, "target_hours query param must reach the service")
			return fullScore(85), nil
		},
	}
	store := &stubSnapshotStore{prior: &domain.PresenceHealthSnapshot{Composite: 80}}
	h, err := NewPresenceHealthHandler(scorer, store)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence-health?target_hours=48", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetPresenceHealth(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp openapi.PresenceHealthResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))

	require.NotNil(t, resp.Composite)
	assert.Equal(t, 85, *resp.Composite)
	require.NotNil(t, resp.TrendDelta)
	assert.Equal(t, 5, *resp.TrendDelta, "trendDelta = current 85 - prior 80")
	require.NotNil(t, resp.SubScores.RatingScore)
	assert.Equal(t, 90, *resp.SubScores.RatingScore)
	require.NotNil(t, resp.SubScores.SyncScore)
	assert.Equal(t, 60, *resp.SubScores.SyncScore)
	require.NotNil(t, resp.TopRecommendation)
	assert.Equal(t, openapi.PresenceRecommendationArea("sync"), resp.TopRecommendation.Area)
	assert.Equal(t, "presenceHealth.recommendation.sync", resp.TopRecommendation.Message)
	assert.Equal(t, 4, resp.TopRecommendation.PotentialGain)
	assert.False(t, resp.ComputedAt.IsZero())

	require.Len(t, store.upserted, 1, "current-week snapshot must be lazily upserted on read")
	assert.Equal(t, 85, store.upserted[0].Composite)
}

// TestGetPresenceHealth_TrendNullWhenNoPrior proves trendDelta is null when no
// prior-week snapshot exists — and the lazy same-week upsert still happens.
func TestGetPresenceHealth_TrendNullWhenNoPrior(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	scorer := &stubPresenceScorer{
		scoreFn: func(context.Context, uuid.UUID, int) (service.PresenceHealthScore, error) {
			return fullScore(70), nil
		},
	}
	store := &stubSnapshotStore{prior: nil} // no prior week.
	h, _ := NewPresenceHealthHandler(scorer, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence-health", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetPresenceHealth(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp openapi.PresenceHealthResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Nil(t, resp.TrendDelta, "trendDelta must be null with no prior-week snapshot")
	require.NotNil(t, resp.Composite)
	assert.Equal(t, 70, *resp.Composite)
}

// TestGetPresenceHealth_EmptyStateNullable proves the empty state serializes
// composite/trendDelta/recommendation as JSON null and does NOT write a snapshot
// (no composite to stamp).
func TestGetPresenceHealth_EmptyStateNullable(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	scorer := &stubPresenceScorer{
		scoreFn: func(context.Context, uuid.UUID, int) (service.PresenceHealthScore, error) {
			return service.PresenceHealthScore{
				Weights: service.PresenceWeights{},
			}, nil
		},
	}
	store := &stubSnapshotStore{}
	h, _ := NewPresenceHealthHandler(scorer, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence-health", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetPresenceHealth(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp openapi.PresenceHealthResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Nil(t, resp.Composite)
	assert.Nil(t, resp.TrendDelta)
	assert.Nil(t, resp.TopRecommendation)
	assert.Nil(t, resp.SubScores.RatingScore)
	assert.Empty(t, store.upserted, "no snapshot is stamped when there is no composite")
}

// TestGetPresenceHealth_Forbidden proves the endpoint requires content:read —
// the scorer must not be called and the trend snapshot must not be read.
func TestGetPresenceHealth_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{},
	})
	scorer := &stubPresenceScorer{
		scoreFn: func(context.Context, uuid.UUID, int) (service.PresenceHealthScore, error) {
			t.Fatal("scorer must not be called without content:read")
			return service.PresenceHealthScore{}, nil
		},
	}
	h, _ := NewPresenceHealthHandler(scorer, &stubSnapshotStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence-health", http.NoBody)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.GetPresenceHealth(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// TestGetPresenceHealth_BusinessNotFound proves a service ErrBusinessNotFound
// maps to 404.
func TestGetPresenceHealth_BusinessNotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	scorer := &stubPresenceScorer{
		scoreFn: func(context.Context, uuid.UUID, int) (service.PresenceHealthScore, error) {
			return service.PresenceHealthScore{}, domain.ErrBusinessNotFound
		},
	}
	h, _ := NewPresenceHealthHandler(scorer, &stubSnapshotStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence-health", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetPresenceHealth(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// TestGetPresenceHealth_PriorWeekReadExcludesCurrent proves the trend read asks
// for a week strictly before the current ISO-week, so a lazy same-week upsert
// can never become its own baseline.
func TestGetPresenceHealth_PriorWeekReadExcludesCurrent(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	fixedNow := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC) // ISO week 10 of 2026.
	scorer := &stubPresenceScorer{
		scoreFn: func(context.Context, uuid.UUID, int) (service.PresenceHealthScore, error) {
			return fullScore(88), nil
		},
	}
	store := &stubSnapshotStore{}
	h, _ := NewPresenceHealthHandler(scorer, store)
	h.now = func() time.Time { return fixedNow }

	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence-health", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetPresenceHealth(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "2026-W10", store.priorWeek, "trend must query strictly-prior weeks relative to the current ISO-week")
	assert.Equal(t, businessID, store.priorBizID)
	require.Len(t, store.upserted, 1)
	assert.Equal(t, "2026-W10", store.upserted[0].ISOWeek, "lazy upsert stamps the current ISO-week")
}
