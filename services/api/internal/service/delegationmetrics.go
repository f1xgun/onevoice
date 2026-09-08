package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

const (
	delegationMetricWeeks = 8
	daysPerWeek           = 7
)

type DelegationMetricWeek struct {
	WeekStart        time.Time
	Replied          int
	AcceptedUnedited int
	Edited           int
	Unknown          int
}

type DelegationMetrics struct {
	From             time.Time
	To               time.Time
	Replied          int
	AcceptedUnedited int
	Edited           int
	Unknown          int
	Measurable       int
	Weeks            []DelegationMetricWeek
}

func mondayUTC(t time.Time) time.Time {
	t = t.UTC()
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(d.Weekday()) + daysPerWeek - 1) % daysPerWeek
	return d.AddDate(0, 0, -offset)
}

func (s *reviewService) DelegationMetrics(ctx context.Context, businessID uuid.UUID) (DelegationMetrics, error) {
	current := mondayUTC(time.Now())
	from := current.AddDate(0, 0, -daysPerWeek*(delegationMetricWeeks-1))
	to := current.AddDate(0, 0, daysPerWeek)
	rows, err := s.repo.AggregateDelegationMetrics(ctx, businessID.String(), from, to)
	if err != nil {
		return DelegationMetrics{}, fmt.Errorf("aggregate delegation metrics: %w", err)
	}
	return buildDelegationMetrics(rows, current), nil
}

func buildDelegationMetrics(rows []domain.ReviewDelegationWeek, current time.Time) DelegationMetrics {
	current = mondayUTC(current)
	from := current.AddDate(0, 0, -daysPerWeek*(delegationMetricWeeks-1))
	to := current.AddDate(0, 0, daysPerWeek)
	byWeek := make(map[time.Time]DelegationMetricWeek, len(rows))
	for _, row := range rows {
		unknown := row.Replied - row.AcceptedUnedited - row.Edited
		if unknown < 0 {
			unknown = 0
		}
		byWeek[row.WeekStart.UTC()] = DelegationMetricWeek{WeekStart: row.WeekStart.UTC(), Replied: row.Replied, AcceptedUnedited: row.AcceptedUnedited, Edited: row.Edited, Unknown: unknown}
	}
	out := DelegationMetrics{From: from, To: to, Weeks: make([]DelegationMetricWeek, 0, delegationMetricWeeks)}
	for i := 0; i < delegationMetricWeeks; i++ {
		start := from.AddDate(0, 0, i*daysPerWeek)
		week := byWeek[start]
		week.WeekStart = start
		out.Weeks = append(out.Weeks, week)
		out.Replied += week.Replied
		out.AcceptedUnedited += week.AcceptedUnedited
		out.Edited += week.Edited
		out.Unknown += week.Unknown
	}
	out.Measurable = out.AcceptedUnedited + out.Edited
	return out
}
