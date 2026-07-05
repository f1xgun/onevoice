// Package presencehealth runs the weekly presence-health snapshot worker: one
// pass over the active-business fleet that computes each business's composite
// score and upserts this ISO-week's snapshot, idempotently per (business,
// week). The snapshot table is what the GET /businesses/{id}/presence-health
// trend reads a prior week from to derive the week-over-week delta.
//
// It mirrors creditgrant.Service: enumeration decides the fleet, a single
// business's failure is logged and skipped so one bad row never aborts the
// pass, and only an enumeration failure aborts. The UNIQUE (business_id,
// iso_week) constraint plus the repository upsert make a re-run in the same week
// a cheap no-op — the immediate first pass on deploy is safe.
package presencehealth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// ISOWeekKey formats t as the 'YYYY-Www' ISO-week key stored on
// presence_health_snapshots.iso_week. It is lexically ordered, so the trend read
// can select the most-recent prior week with a plain string comparison. Shared
// with the handler's lazy same-week upsert so both stamp the identical key.
func ISOWeekKey(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// Enumerator lists the businesses to snapshot.
// domain.PresenceHealthSnapshotRepository satisfies it.
type Enumerator interface {
	EnumerateActiveBusinessIDs(ctx context.Context) ([]uuid.UUID, error)
}

// Scorer computes one business's composite presence-health score.
// *service.PresenceHealthService satisfies it.
type Scorer interface {
	Score(ctx context.Context, businessID uuid.UUID, targetHours int) (service.PresenceHealthScore, error)
}

// Upserter stamps one week's snapshot idempotently.
// domain.PresenceHealthSnapshotRepository satisfies it.
type Upserter interface {
	Upsert(ctx context.Context, snap domain.PresenceHealthSnapshot) error
}

// Service stamps the weekly presence-health snapshot across the active-business
// fleet.
type Service struct {
	enum     Enumerator
	scorer   Scorer
	upserter Upserter
	log      *slog.Logger
	now      func() time.Time
}

// New constructs the snapshot worker. A nil logger falls back to slog.Default().
func New(enum Enumerator, scorer Scorer, upserter Upserter, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{enum: enum, scorer: scorer, upserter: upserter, log: log, now: time.Now}
}

// SnapshotAll runs one snapshot pass over every active business for the current
// ISO-week and returns how many snapshots were stamped. A business whose score
// has no composite (no reviews yet) is skipped — an empty presence has no
// meaningful weekly point to trend. A single business's score or upsert failing
// is logged and skipped so one bad row never aborts the fleet-wide pass; only an
// enumeration failure aborts.
func (s *Service) SnapshotAll(ctx context.Context) (int, error) {
	week := ISOWeekKey(s.now().UTC())

	ids, err := s.enum.EnumerateActiveBusinessIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("presencehealth: enumerate active businesses: %w", err)
	}

	stamped := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return stamped, err
		}
		score, err := s.scorer.Score(ctx, id, 0)
		if err != nil {
			s.log.WarnContext(ctx, "presencehealth: score failed", "business_id", id, "week", week, "err", err)
			continue
		}
		if score.Composite == nil {
			continue
		}
		snap := SnapshotFromScore(id, week, score, s.now().UTC())
		if err := s.upserter.Upsert(ctx, snap); err != nil {
			s.log.WarnContext(ctx, "presencehealth: upsert snapshot failed", "business_id", id, "week", week, "err", err)
			continue
		}
		stamped++
	}
	return stamped, nil
}

// SnapshotFromScore builds a snapshot row from a computed score. It is only
// called with a non-nil composite (SnapshotAll skips empty presences). Absent
// review-derived sub-scores default to 0 in the stored row (the columns are NOT
// NULL); an absent sync sub-score is preserved as NULL so a channel-less
// business is not recorded as sync=0.
func SnapshotFromScore(businessID uuid.UUID, week string, score service.PresenceHealthScore, createdAt time.Time) domain.PresenceHealthSnapshot {
	snap := domain.PresenceHealthSnapshot{
		ID:         uuid.New(),
		BusinessID: businessID,
		ISOWeek:    week,
		Composite:  *score.Composite,
		SyncScore:  score.SubScores.Sync,
		CreatedAt:  createdAt,
	}
	if score.SubScores.Rating != nil {
		snap.RatingScore = *score.SubScores.Rating
	}
	if score.SubScores.SLA != nil {
		snap.SLAScore = *score.SubScores.SLA
	}
	if score.SubScores.Coverage != nil {
		snap.CoverageScore = *score.SubScores.Coverage
	}
	return snap
}
