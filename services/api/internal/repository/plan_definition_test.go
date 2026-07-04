package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func newPlanDefinitionMock(t *testing.T) (pgxmock.PgxPoolIface, domain.PlanDefinitionRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewPlanDefinitionRepository(mock)
}

func planRow(code, tier string, credits int, dailyCap float64) *pgxmock.Rows {
	return pgxmock.NewRows(planDefinitionColumns).AddRow(
		code, code, 0.0, credits, 0.0, dailyCap, -1, -1, tier, true, 0,
		time.Now().UTC(), time.Now().UTC(),
	)
}

func TestPlanDefinition_GetByCode_Found(t *testing.T) {
	mock, repo := newPlanDefinitionMock(t)

	mock.ExpectQuery(`SELECT .* FROM plan_definitions WHERE`).
		WithArgs("pro").
		WillReturnRows(planRow("pro", "pro", 2000, 50))

	got, err := repo.GetByCode(context.Background(), "pro")
	require.NoError(t, err)
	require.Equal(t, "pro", got.Code)
	require.Equal(t, "pro", got.RateLimitTier)
	require.Equal(t, 2000, got.MonthlyCredits)
	require.InDelta(t, 50.0, got.DailyLLMUSDCap, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlanDefinition_GetByCode_NotFound(t *testing.T) {
	mock, repo := newPlanDefinitionMock(t)

	mock.ExpectQuery(`SELECT .* FROM plan_definitions WHERE`).
		WithArgs("ghost").
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetByCode(context.Background(), "ghost")
	require.ErrorIs(t, err, domain.ErrPlanNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlanDefinition_ListActive_OrdersBySortOrder(t *testing.T) {
	mock, repo := newPlanDefinitionMock(t)

	rows := pgxmock.NewRows(planDefinitionColumns).
		AddRow("free", "free", 0.0, 100, 0.0, 1.0, 1, 1, "free", true, 0, time.Now().UTC(), time.Now().UTC()).
		AddRow("pro", "pro", 1990.0, 2000, 2.0, 50.0, -1, 5, "pro", true, 1, time.Now().UTC(), time.Now().UTC())
	mock.ExpectQuery(`SELECT .* FROM plan_definitions WHERE .* ORDER BY sort_order`).
		WithArgs(true).
		WillReturnRows(rows)

	got, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "free", got[0].Code)
	require.Equal(t, "pro", got[1].Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
