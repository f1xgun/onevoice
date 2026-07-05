package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestChannelDemandSignal_Insert_WithNote(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewChannelDemandSignalRepository(mock)

	bizID := uuid.New()
	note := "нужен Авито"
	mock.ExpectExec(`INSERT INTO channel_demand_signals`).
		WithArgs(bizID, "avito", &note).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.Insert(context.Background(), ChannelDemandSignalRow{
		BusinessID: bizID,
		Channel:    "avito",
		Note:       &note,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelDemandSignal_Insert_NullNote(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewChannelDemandSignalRepository(mock)

	bizID := uuid.New()
	mock.ExpectExec(`INSERT INTO channel_demand_signals`).
		WithArgs(bizID, "ozon", (*string)(nil)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.Insert(context.Background(), ChannelDemandSignalRow{
		BusinessID: bizID,
		Channel:    "ozon",
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelDemandSignal_SummaryByBusiness_ScopedToBusiness(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewChannelDemandSignalRepository(mock)

	bizID := uuid.New()
	mock.ExpectQuery(`SELECT channel, COUNT\(\*\) FROM channel_demand_signals WHERE business_id = \$1 GROUP BY channel`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(mock.NewRows([]string{"channel", "count"}).
			AddRow("avito", 3).
			AddRow("ozon", 1))

	got, err := repo.SummaryByBusiness(context.Background(), bizID)
	require.NoError(t, err)
	require.Equal(t, []ChannelDemandCount{
		{Channel: "avito", Count: 3},
		{Channel: "ozon", Count: 1},
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
