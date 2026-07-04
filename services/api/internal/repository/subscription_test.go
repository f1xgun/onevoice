package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func newSubscriptionMock(t *testing.T) (pgxmock.PgxPoolIface, domain.SubscriptionRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewSubscriptionRepository(mock)
}

func TestSubscription_ActiveByBusiness_Found(t *testing.T) {
	mock, repo := newSubscriptionMock(t)
	bizID := uuid.New()
	subID := uuid.New()

	rows := pgxmock.NewRows(subscriptionColumns).AddRow(
		subID, bizID, nil, "pro", domain.SubscriptionStatusActive,
		nil, nil, nil, nil, false, time.Now().UTC(), time.Now().UTC(),
	)
	mock.ExpectQuery(`SELECT .* FROM subscriptions WHERE`).
		WithArgs(pgxmock.AnyArg(), domain.SubscriptionStatusActive).
		WillReturnRows(rows)

	got, err := repo.ActiveByBusiness(context.Background(), bizID)
	require.NoError(t, err)
	require.Equal(t, "pro", got.PlanCode)
	require.Equal(t, bizID, got.BusinessID)
	require.Equal(t, domain.SubscriptionStatusActive, got.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscription_ActiveByBusiness_NotFound(t *testing.T) {
	mock, repo := newSubscriptionMock(t)

	mock.ExpectQuery(`SELECT .* FROM subscriptions WHERE`).
		WithArgs(pgxmock.AnyArg(), domain.SubscriptionStatusActive).
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.ActiveByBusiness(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrSubscriptionNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscription_Upsert_InsertsWithConflictClause(t *testing.T) {
	mock, repo := newSubscriptionMock(t)

	mock.ExpectExec(`INSERT INTO subscriptions .* ON CONFLICT \(business_id\) WHERE status = 'active' DO UPDATE`).
		WithArgs(anyArgs(10)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Upsert(context.Background(), &domain.Subscription{
		BusinessID: uuid.New(),
		PlanCode:   "pro",
		Status:     domain.SubscriptionStatusActive,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
