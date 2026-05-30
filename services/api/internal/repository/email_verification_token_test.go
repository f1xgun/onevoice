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

// newEmailVerificationTokenRepoMock — mirrors newPasswordResetTokenRepoMock
// to keep repo tests stylistically uniform (pgxmock instead of
// real Postgres per the deviation precedent).
func newEmailVerificationTokenRepoMock(t *testing.T) (pgxmock.PgxPoolIface, *EmailVerificationTokenRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewEmailVerificationTokenRepository(mock)
}

// --- Insert -------------------------------------------------------------

// TestEmailVerificationTokenRepository_Insert_StoresHashNotPlaintext
// asserts the repo writes the hash bytes (not the plaintext) verbatim into
// the token_hash column via the expected INSERT shape.
func TestEmailVerificationTokenRepository_Insert_StoresHashNotPlaintext(t *testing.T) {
	mock, repo := newEmailVerificationTokenRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	email := "alice@example.com"
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	expiresAt := time.Now().Add(72 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO email_verification_tokens`).
		WithArgs(userID, email, hash, expiresAt).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Insert(ctx, tx, userID, email, hash, expiresAt))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- ConsumeAtomic ------------------------------------------------------

func TestEmailVerificationTokenRepository_ConsumeAtomic_HappyPath(t *testing.T) {
	mock, repo := newEmailVerificationTokenRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	email := "happy@example.com"
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE email_verification_tokens`).
		WithArgs(hash).
		WillReturnRows(mock.NewRows([]string{"user_id", "email"}).AddRow(userID, email))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	gotID, gotEmail, err := repo.ConsumeAtomic(ctx, tx, hash)
	require.NoError(t, err)
	require.Equal(t, userID, gotID)
	require.Equal(t, email, gotEmail)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailVerificationTokenRepository_ConsumeAtomic_SecondAttemptFails
// asserts that ErrVerifyTokenInvalid is returned when the UPDATE matches
// zero rows — by the second consume, consumed_at IS NULL no longer holds.
func TestEmailVerificationTokenRepository_ConsumeAtomic_SecondAttemptFails(t *testing.T) {
	mock, repo := newEmailVerificationTokenRepoMock(t)
	ctx := context.Background()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE email_verification_tokens`).
		WithArgs(hash).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	_, _, err = repo.ConsumeAtomic(ctx, tx, hash)
	require.ErrorIs(t, err, domain.ErrVerifyTokenInvalid)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailVerificationTokenRepository_ConsumeAtomic_ExpiredFails — same
// mechanism as SecondAttemptFails: expires_at > NOW filter drops the
// row, UPDATE returns zero rows. PITFALLS §1.1 collapse.
func TestEmailVerificationTokenRepository_ConsumeAtomic_ExpiredFails(t *testing.T) {
	mock, repo := newEmailVerificationTokenRepoMock(t)
	ctx := context.Background()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE email_verification_tokens`).
		WithArgs(hash).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	_, _, err = repo.ConsumeAtomic(ctx, tx, hash)
	require.ErrorIs(t, err, domain.ErrVerifyTokenInvalid)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEmailVerificationTokenRepository_ConsumeAtomic_UnknownHashFails —
// pgx.ErrNoRows → ErrVerifyTokenInvalid same as the other no-match modes.
func TestEmailVerificationTokenRepository_ConsumeAtomic_UnknownHashFails(t *testing.T) {
	mock, repo := newEmailVerificationTokenRepoMock(t)
	ctx := context.Background()
	hash := []byte("nonexistent-hash----------------")

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE email_verification_tokens`).
		WithArgs(hash).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	_, _, err = repo.ConsumeAtomic(ctx, tx, hash)
	require.ErrorIs(t, err, domain.ErrVerifyTokenInvalid)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- LookupExpired ------------------------------------------------------

func TestEmailVerificationTokenRepository_LookupExpired_ReturnsTrueWhenExpired(t *testing.T) {
	mock, repo := newEmailVerificationTokenRepoMock(t)
	ctx := context.Background()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(hash).
		WillReturnRows(mock.NewRows([]string{"exists"}).AddRow(true))

	got, err := repo.LookupExpired(ctx, hash)
	require.NoError(t, err)
	require.True(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmailVerificationTokenRepository_LookupExpired_ReturnsFalseWhenAbsent(t *testing.T) {
	mock, repo := newEmailVerificationTokenRepoMock(t)
	ctx := context.Background()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(hash).
		WillReturnRows(mock.NewRows([]string{"exists"}).AddRow(false))

	got, err := repo.LookupExpired(ctx, hash)
	require.NoError(t, err)
	require.False(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- InvalidateAllForUser -----------------------------------------------

// TestEmailVerificationTokenRepository_InvalidateAllForUser_SetsConsumedAt
// asserts the UPDATE WHERE user_id=$1 AND consumed_at IS NULL fires and the
// repo returns nil on success. The "subsequent consume fails" half of the
// behavior is exercised end-to-end by integration tests; the repo-level
// contract here is just that the SQL is shaped correctly.
func TestEmailVerificationTokenRepository_InvalidateAllForUser_SetsConsumedAt(t *testing.T) {
	mock, repo := newEmailVerificationTokenRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE email_verification_tokens`).
		WithArgs(userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 3))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.InvalidateAllForUser(ctx, tx, userID))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}
