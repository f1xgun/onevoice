package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
)

// WeeklyValueRecapMinOperations prevents a quiet week from becoming a discouraging banner.
const WeeklyValueRecapMinOperations = 2

const weeklyValueRecapOrganizationTimeout = 5 * time.Second

type WeeklyValueRecapService struct {
	repo   domain.WeeklyValueRecapRepository
	source domain.WeeklyValueRecapSource
}

func NewWeeklyValueRecapService(repo domain.WeeklyValueRecapRepository, source domain.WeeklyValueRecapSource) *WeeklyValueRecapService {
	return &WeeklyValueRecapService{repo: repo, source: source}
}

func PreviousCompletedUTCWeek(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	currentMonday := time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.UTC)
	return currentMonday.AddDate(0, 0, -7), currentMonday
}

func (s *WeeklyValueRecapService) Latest(ctx context.Context, businessID uuid.UUID, now time.Time) (*domain.WeeklyValueRecap, error) {
	from, _ := PreviousCompletedUTCWeek(now)
	v, err := s.repo.GetForWeek(ctx, businessID, from)
	if err != nil {
		return nil, fmt.Errorf("get current weekly recap: %w", err)
	}
	if v == nil || v.Total() < WeeklyValueRecapMinOperations {
		return nil, nil
	}
	return v, nil
}

// Sweep aggregates each active business independently; one corrupt tenant cannot block the rest.
func (s *WeeklyValueRecapService) Sweep(ctx context.Context, now time.Time) (int, error) {
	ids, err := s.repo.EnumerateActiveBusinessIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("enumerate weekly recap businesses: %w", err)
	}
	from, to := PreviousCompletedUTCWeek(now)
	written := 0
	failures := make([]error, 0)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		organizationCtx, cancel := context.WithTimeout(ctx, weeklyValueRecapOrganizationTimeout)
		v, err := s.source.CountWeeklyValue(organizationCtx, id, from, to)
		if err != nil {
			cancel()
			slog.WarnContext(ctx, "weekly recap aggregation failed", "business_id", id, "error", err)
			failures = append(failures, fmt.Errorf("aggregate business %s: %w", id, err))
			continue
		}
		if err := s.repo.Upsert(organizationCtx, v); err != nil {
			cancel()
			slog.WarnContext(ctx, "weekly recap persist failed", "business_id", id, "error", err)
			failures = append(failures, fmt.Errorf("persist business %s: %w", id, err))
			continue
		}
		cancel()
		written++
	}
	return written, errors.Join(failures...)
}
