package service

import (
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/stretchr/testify/require"
)

func TestBuildDelegationMetrics_ExactWeeksAndKnownDenominator(t *testing.T) {
	current := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	rows := []domain.ReviewDelegationWeek{
		{WeekStart: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), Replied: 4, AcceptedUnedited: 1, Edited: 2},
		{WeekStart: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), Replied: 3, AcceptedUnedited: 2, Edited: 1},
	}
	got := buildDelegationMetrics(rows, current)
	require.Equal(t, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), got.From)
	require.Equal(t, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), got.To)
	require.Len(t, got.Weeks, 8)
	require.Equal(t, 7, got.Replied)
	require.Equal(t, 3, got.AcceptedUnedited)
	require.Equal(t, 3, got.Edited)
	require.Equal(t, 1, got.Unknown)
	require.Equal(t, 6, got.Measurable)
	require.Equal(t, 0, got.Weeks[1].Replied)
}
