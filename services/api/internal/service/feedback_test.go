package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// anyArgs returns n pgxmock.AnyArg matchers for inserts whose exact argument
// values are not the subject under test.
func anyArgs(n int) []any {
	a := make([]any, n)
	for i := range a {
		a[i] = pgxmock.AnyArg()
	}
	return a
}

type fakeUserLookup struct {
	user *domain.User
	err  error
}

func (f fakeUserLookup) GetByID(_ context.Context, _ uuid.UUID) (*domain.User, error) {
	return f.user, f.err
}

func TestFeedbackService_Submit_NoNotify(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })

	repo := repository.NewProductFeedbackRepository(mock)
	outbox := repository.NewEmailOutboxRepository(mock)
	svc := NewFeedbackService(mock, repo, outbox, fakeUserLookup{}, "")

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO product_feedback`).
		WithArgs(anyArgs(8)...).
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err = svc.Submit(context.Background(), uuid.New(), FeedbackInput{Category: "bug", Message: "broken"})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFeedbackService_Submit_WithNotify(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })

	repo := repository.NewProductFeedbackRepository(mock)
	outbox := repository.NewEmailOutboxRepository(mock)
	users := fakeUserLookup{user: &domain.User{Email: "alice@example.com"}}
	svc := NewFeedbackService(mock, repo, outbox, users, "founder@onevoice.app")

	rating := int16(4)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO product_feedback`).
		WithArgs(anyArgs(8)...).
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectQuery(`INSERT INTO email_outbox`).
		WithArgs(anyArgs(5)...).
		WillReturnRows(mock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err = svc.Submit(context.Background(), uuid.New(), FeedbackInput{Category: "idea", Message: "add x", Rating: &rating})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
