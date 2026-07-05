package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// newOwnerLinkTokenRepoMock returns a fresh pgxmock pool + repo. Mirrors
// newPasswordResetTokenRepoMock; the owner-link repo drives the pool directly
// (no caller-supplied tx), so no ExpectBegin/Commit is needed.
func newOwnerLinkTokenRepoMock(t *testing.T) (pgxmock.PgxPoolIface, *TelegramOwnerLinkTokenRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewTelegramOwnerLinkTokenRepository(mock)
}

// --- Insert -------------------------------------------------------------

func TestOwnerLinkTokenRepository_Insert_RoundTrip(t *testing.T) {
	mock, repo := newOwnerLinkTokenRepoMock(t)
	ctx := context.Background()
	businessID := uuid.New()
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	expiresAt := time.Now().Add(10 * time.Minute)

	mock.ExpectExec(`INSERT INTO telegram_owner_link_tokens`).
		WithArgs(businessID, hash, expiresAt).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	require.NoError(t, repo.Insert(ctx, businessID, hash, expiresAt))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestOwnerLinkTokenRepository_Insert_DuplicateHashFails maps sqlstate 23505 onto
// ErrLinkTokenCollision so the mint path may retry with a fresh token.
func TestOwnerLinkTokenRepository_Insert_DuplicateHashFails(t *testing.T) {
	mock, repo := newOwnerLinkTokenRepoMock(t)
	ctx := context.Background()
	businessID := uuid.New()
	hash := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	expiresAt := time.Now().Add(10 * time.Minute)

	mock.ExpectExec(`INSERT INTO telegram_owner_link_tokens`).
		WithArgs(businessID, hash, expiresAt).
		WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"})

	err := repo.Insert(ctx, businessID, hash, expiresAt)
	require.ErrorIs(t, err, domain.ErrLinkTokenCollision)
}

// --- ConsumeAtomic ------------------------------------------------------

func TestOwnerLinkTokenRepository_ConsumeAtomic_HappyPath(t *testing.T) {
	mock, repo := newOwnerLinkTokenRepoMock(t)
	ctx := context.Background()
	businessID := uuid.New()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectQuery(`UPDATE telegram_owner_link_tokens`).
		WithArgs(hash).
		WillReturnRows(mock.NewRows([]string{"business_id"}).AddRow(businessID))

	gotID, err := repo.ConsumeAtomic(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, businessID, gotID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestOwnerLinkTokenRepository_ConsumeAtomic_CollapsedFailures asserts that
// consumed | expired | unknown all collapse to ErrLinkTokenInvalid — the atomic
// UPDATE returns zero rows for every one, so the repository never distinguishes
// the failure mode (no enumeration).
func TestOwnerLinkTokenRepository_ConsumeAtomic_CollapsedFailures(t *testing.T) {
	for _, name := range []string{"already_consumed", "expired", "unknown"} {
		t.Run(name, func(t *testing.T) {
			mock, repo := newOwnerLinkTokenRepoMock(t)
			ctx := context.Background()
			hash := []byte("hash-32-bytes-padded-with-zeros!")

			mock.ExpectQuery(`UPDATE telegram_owner_link_tokens`).
				WithArgs(hash).
				WillReturnError(pgx.ErrNoRows)

			_, err := repo.ConsumeAtomic(ctx, hash)
			require.ErrorIs(t, err, domain.ErrLinkTokenInvalid)
		})
	}
}

// --- InvalidateAllForBusiness -------------------------------------------

func TestOwnerLinkTokenRepository_InvalidateAllForBusiness(t *testing.T) {
	mock, repo := newOwnerLinkTokenRepoMock(t)
	ctx := context.Background()
	businessID := uuid.New()

	mock.ExpectExec(`UPDATE telegram_owner_link_tokens`).
		WithArgs(businessID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	require.NoError(t, repo.InvalidateAllForBusiness(ctx, businessID))
	require.NoError(t, mock.ExpectationsWereMet())
}
