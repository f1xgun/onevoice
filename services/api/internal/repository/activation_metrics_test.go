package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestActivationRepository_RecentActivationUsesOwnerMembership(t *testing.T) {
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	since := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	pool.ExpectQuery(`FROM business_members bm\s+JOIN businesses b`).
		WithArgs(since, domain.IntegrationStatusActive, domain.SystemRoleOwnerID, "active").
		WillReturnRows(pgxmock.NewRows([]string{"signups", "activated"}).AddRow(5, 2))

	stats, err := NewActivationRepository(pool).RecentActivation(context.Background(), since)
	require.NoError(t, err)
	require.Equal(t, ActivationStats{Signups: 5, Activated: 2}, stats)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestActivationRepository_RecentActivationWrapsQueryError(t *testing.T) {
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	since := time.Now().UTC()
	pool.ExpectQuery(`FROM business_members bm`).
		WithArgs(since, domain.IntegrationStatusActive, domain.SystemRoleOwnerID, "active").
		WillReturnError(errors.New("database unavailable"))

	_, err = NewActivationRepository(pool).RecentActivation(context.Background(), since)
	require.ErrorContains(t, err, "query activation funnel: database unavailable")
	require.NoError(t, pool.ExpectationsWereMet())
}
