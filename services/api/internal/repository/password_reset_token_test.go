package repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// newPasswordResetTokenRepoMock returns a fresh pgxmock pool + repo.
// Mirrors newEmailOutboxRepoMock to keep repository tests
// stylistically uniform.
func newPasswordResetTokenRepoMock(t *testing.T) (pgxmock.PgxPoolIface, *PasswordResetTokenRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewPasswordResetTokenRepository(mock)
}

// --- Insert -------------------------------------------------------------

func TestPasswordResetTokenRepository_Insert_RoundTrip(t *testing.T) {
	mock, repo := newPasswordResetTokenRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	expiresAt := time.Now().Add(30 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO password_reset_tokens`).
		WithArgs(userID, "owner@example.test", hash, expiresAt).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Insert(ctx, tx, userID, "owner@example.test", hash, expiresAt))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestPasswordResetTokenRepository_Insert_DuplicateHashFails maps the
// Postgres UNIQUE-violation sqlstate 23505 onto ErrResetTokenCollision so
// the service may retry on the (astronomically improbable) duplicate.
func TestPasswordResetTokenRepository_Insert_DuplicateHashFails(t *testing.T) {
	mock, repo := newPasswordResetTokenRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	hash := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	expiresAt := time.Now().Add(30 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO password_reset_tokens`).
		WithArgs(userID, "owner@example.test", hash, expiresAt).
		WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"})

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	err = repo.Insert(ctx, tx, userID, "owner@example.test", hash, expiresAt)
	require.ErrorIs(t, err, domain.ErrResetTokenCollision)
}

// --- ConsumeAtomic ------------------------------------------------------

func TestPasswordResetTokenRepository_ConsumeAtomic_HappyPath(t *testing.T) {
	mock, repo := newPasswordResetTokenRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE password_reset_tokens`).
		WithArgs(hash).
		WillReturnRows(mock.NewRows([]string{"user_id"}).AddRow(userID))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	gotID, err := repo.ConsumeAtomic(ctx, tx, hash)
	require.NoError(t, err)
	require.Equal(t, userID, gotID)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestPasswordResetTokenRepository_ConsumeAtomic_AlreadyConsumed asserts
// that the second consume of a token returns ErrResetTokenInvalid.
// Zero rows returned from the atomic UPDATE is the canonical signal.
func TestPasswordResetTokenRepository_ConsumeAtomic_AlreadyConsumed(t *testing.T) {
	mock, repo := newPasswordResetTokenRepoMock(t)
	ctx := context.Background()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE password_reset_tokens`).
		WithArgs(hash).
		WillReturnError(pgx.ErrNoRows)

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	_, err = repo.ConsumeAtomic(ctx, tx, hash)
	require.ErrorIs(t, err, domain.ErrResetTokenInvalid)
}

// TestPasswordResetTokenRepository_ConsumeAtomic_Expired asserts that an
// expired token (expires_at < NOW) is rejected with the SAME sentinel
// as already-consumed and non-existent — PITFALLS §1.1 forbids
// distinguishing failure modes at the repository layer.
func TestPasswordResetTokenRepository_ConsumeAtomic_Expired(t *testing.T) {
	mock, repo := newPasswordResetTokenRepoMock(t)
	ctx := context.Background()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE password_reset_tokens`).
		WithArgs(hash).
		WillReturnError(pgx.ErrNoRows)

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	_, err = repo.ConsumeAtomic(ctx, tx, hash)
	require.ErrorIs(t, err, domain.ErrResetTokenInvalid)
}

// TestPasswordResetTokenRepository_ConsumeAtomic_NonExistent asserts the
// unknown-hash case collapses to ErrResetTokenInvalid (PITFALLS §1.1).
func TestPasswordResetTokenRepository_ConsumeAtomic_NonExistent(t *testing.T) {
	mock, repo := newPasswordResetTokenRepoMock(t)
	ctx := context.Background()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE password_reset_tokens`).
		WithArgs(hash).
		WillReturnError(pgx.ErrNoRows)

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	_, err = repo.ConsumeAtomic(ctx, tx, hash)
	require.ErrorIs(t, err, domain.ErrResetTokenInvalid)
}

// TestPasswordResetTokenRepository_ConsumeAtomic_ConcurrentRace verifies
// the at-most-one-winner guarantee under concurrent access. 50 goroutines
// each attempt to consume the same token; we simulate Postgres's
// row-level lock by allowing the first ExpectQuery to win and every
// subsequent attempt to receive pgx.ErrNoRows. The test asserts exactly
// one success and 49 ErrResetTokenInvalid responses — the contract the
// real Postgres atomic UPDATE delivers in production.
func TestPasswordResetTokenRepository_ConsumeAtomic_ConcurrentRace(t *testing.T) {
	mock, repo := newPasswordResetTokenRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	hash := []byte("hash-32-bytes-padded-with-zeros!")

	const N = 50
	mock.MatchExpectationsInOrder(false)
	for i := 0; i < N; i++ {
		mock.ExpectBegin()
		if i == 0 {
			mock.ExpectQuery(`UPDATE password_reset_tokens`).
				WithArgs(hash).
				WillReturnRows(mock.NewRows([]string{"user_id"}).AddRow(userID))
			mock.ExpectCommit()
		} else {
			mock.ExpectQuery(`UPDATE password_reset_tokens`).
				WithArgs(hash).
				WillReturnError(pgx.ErrNoRows)
			mock.ExpectRollback()
		}
	}

	var (
		wg          sync.WaitGroup
		successes   atomic.Int64
		invalids    atomic.Int64
		unexpected  atomic.Int64
		startSignal = make(chan struct{})
	)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-startSignal
			tx, err := mock.Begin(ctx)
			if err != nil {
				unexpected.Add(1)
				return
			}
			_, err = repo.ConsumeAtomic(ctx, tx, hash)
			switch {
			case err == nil:
				successes.Add(1)
				_ = tx.Commit(ctx)
			case errors.Is(err, domain.ErrResetTokenInvalid):
				invalids.Add(1)
				_ = tx.Rollback(ctx)
			default:
				unexpected.Add(1)
				_ = tx.Rollback(ctx)
			}
		}()
	}
	close(startSignal)
	wg.Wait()

	require.Equal(t, int64(1), successes.Load(), "exactly one consumer must succeed")
	require.Equal(t, int64(N-1), invalids.Load(), "all other consumers must see ErrResetTokenInvalid")
	require.Equal(t, int64(0), unexpected.Load(), "no other error class is allowed")
}

// --- InvalidateAllForUser -----------------------------------------------

func TestPasswordResetTokenRepository_InvalidateAllForUser(t *testing.T) {
	mock, repo := newPasswordResetTokenRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE password_reset_tokens`).
		WithArgs(userID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 3))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.InvalidateAllForUser(ctx, tx, userID))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}
