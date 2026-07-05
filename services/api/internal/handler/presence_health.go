package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/presencehealth"
)

// PresenceHealthScorer computes the composite presence-health score for one
// business. *service.PresenceHealthService satisfies it.
type PresenceHealthScorer interface {
	Score(ctx context.Context, businessID uuid.UUID, targetHours int) (service.PresenceHealthScore, error)
}

// PresenceHealthSnapshotStore reads the prior-week snapshot (for the trend
// delta) and upserts the current week's snapshot (the optional lazy write on
// read). domain.PresenceHealthSnapshotRepository satisfies it.
type PresenceHealthSnapshotStore interface {
	GetMostRecentPrior(ctx context.Context, businessID uuid.UUID, currentWeek string) (*domain.PresenceHealthSnapshot, error)
	Upsert(ctx context.Context, snap domain.PresenceHealthSnapshot) error
}

// PresenceHealthHandler serves the read-only composite presence-health score.
type PresenceHealthHandler struct {
	scorer    PresenceHealthScorer
	snapshots PresenceHealthSnapshotStore
	now       func() time.Time
}

// NewPresenceHealthHandler constructs the handler over the scorer + snapshot
// store. A nil dependency is a wiring bug and returns an error.
func NewPresenceHealthHandler(scorer PresenceHealthScorer, snapshots PresenceHealthSnapshotStore) (*PresenceHealthHandler, error) {
	if scorer == nil {
		return nil, errors.New("NewPresenceHealthHandler: scorer cannot be nil")
	}
	if snapshots == nil {
		return nil, errors.New("NewPresenceHealthHandler: snapshots cannot be nil")
	}
	return &PresenceHealthHandler{scorer: scorer, snapshots: snapshots, now: time.Now}, nil
}

// GetPresenceHealth handles GET /businesses/{id}/presence-health. It returns the
// composite 0-100 presence-health score, the week-over-week trend delta, the
// four normalized sub-scores, the weight set used, and a single next-action
// recommendation. Aggregate-only: no author name, review text, or reply text is
// fetched or returned. Authz mirrors the other GET review endpoints:
// RequireBusinessAccess + content:read.
//
// It reads the trend from the most-recent PRIOR-week snapshot and, when the
// current score has a composite, lazily upserts this week's snapshot so the
// trend is fresh without waiting for the weekly worker — the prior-week read
// excludes the current week, so this lazy write can never become its own
// baseline.
func (h *PresenceHealthHandler) GetPresenceHealth(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetPresenceHealth", authz.PermContentRead)
	if !ok {
		return
	}

	targetHours := 0
	if v := r.URL.Query().Get("target_hours"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			targetHours = parsed
		}
	}

	score, err := h.scorer.Score(r.Context(), bc.BusinessID, targetHours)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to compute presence health", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	now := h.now().UTC()
	week := presencehealth.ISOWeekKey(now)

	trendDelta := h.trendDelta(r.Context(), bc.BusinessID, week, score.Composite)

	if score.Composite != nil {
		snap := presencehealth.SnapshotFromScore(bc.BusinessID, week, score, now)
		if err := h.snapshots.Upsert(r.Context(), snap); err != nil {
			slog.WarnContext(r.Context(), "presence health: lazy snapshot upsert failed",
				"business_id", bc.BusinessID, "week", week, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, presenceScoreToOpenAPI(score, trendDelta, now))
}

// trendDelta reads the most-recent prior-week snapshot and returns
// current composite minus that snapshot's composite. It returns nil when the
// current composite is nil (empty state) or no prior-week snapshot exists — a
// read failure degrades to nil rather than failing the whole endpoint.
func (h *PresenceHealthHandler) trendDelta(ctx context.Context, businessID uuid.UUID, currentWeek string, composite *int) *int {
	if composite == nil {
		return nil
	}
	prior, err := h.snapshots.GetMostRecentPrior(ctx, businessID, currentWeek)
	if err != nil {
		slog.WarnContext(ctx, "presence health: prior snapshot read failed",
			"business_id", businessID, "error", err)
		return nil
	}
	if prior == nil {
		return nil
	}
	delta := *composite - prior.Composite
	return &delta
}

// presenceScoreToOpenAPI maps the service score + trend delta to the spec-owned
// wire shape.
func presenceScoreToOpenAPI(s service.PresenceHealthScore, trendDelta *int, computedAt time.Time) openapi.PresenceHealthResponse {
	resp := openapi.PresenceHealthResponse{
		Composite:  s.Composite,
		TrendDelta: trendDelta,
		ComputedAt: computedAt,
		SubScores: openapi.PresenceSubScores{
			RatingScore:   s.SubScores.Rating,
			SlaScore:      s.SubScores.SLA,
			CoverageScore: s.SubScores.Coverage,
			SyncScore:     s.SubScores.Sync,
		},
		Weights: openapi.PresenceWeights{
			Rating:   float32(s.Weights.Rating),
			Sla:      float32(s.Weights.SLA),
			Coverage: float32(s.Weights.Coverage),
			Sync:     float32(s.Weights.Sync),
		},
	}
	if s.Recommendation != nil {
		resp.TopRecommendation = &openapi.PresenceRecommendation{
			Area:          openapi.PresenceRecommendationArea(s.Recommendation.Area),
			Message:       s.Recommendation.MessageKey,
			PotentialGain: s.Recommendation.PotentialGain,
		}
	}
	return resp
}
