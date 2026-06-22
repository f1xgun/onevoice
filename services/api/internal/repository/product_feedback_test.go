package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestProductFeedback_Insert_Commit(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	repo := NewProductFeedbackRepository(mock)

	expected := uuid.New()
	uid := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO product_feedback`).
		WithArgs(anyArgs(8)...).
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(expected))
	mock.ExpectCommit()

	ctx := context.Background()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	got, err := repo.Insert(ctx, tx, ProductFeedbackRow{
		UserID:   &uid,
		Category: "bug",
		Message:  "broken",
		Page:     "/chat",
	})
	require.NoError(t, err)
	require.Equal(t, expected, got)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}
