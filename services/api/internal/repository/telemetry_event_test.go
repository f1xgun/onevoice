package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

// anyArgs returns n pgxmock.AnyArg matchers for queries whose exact argument
// values are not the subject under test (e.g. batch inserts).
func anyArgs(n int) []any {
	a := make([]any, n)
	for i := range a {
		a[i] = pgxmock.AnyArg()
	}
	return a
}

func TestTelemetryEvent_InsertBatch(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewTelemetryEventRepository(mock)

	uid := uuid.New()
	mock.ExpectExec(`INSERT INTO telemetry_events`).
		WithArgs(anyArgs(16)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 2))

	err = repo.InsertBatch(context.Background(), []TelemetryEventRow{
		{UserID: &uid, EventType: "page_view", Action: "load", Page: "/x"},
		{EventType: "click", Action: "save", Metadata: []byte(`{"k":"v"}`)},
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTelemetryEvent_InsertBatch_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewTelemetryEventRepository(mock)

	require.NoError(t, repo.InsertBatch(context.Background(), nil))
	require.NoError(t, mock.ExpectationsWereMet())
}
