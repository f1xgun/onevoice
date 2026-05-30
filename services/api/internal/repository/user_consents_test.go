package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

// newUserConsentsRepoMock — pgxmock harness mirroring
// newEmailVerificationTokenRepoMock for stylistic uniformity with the
// rest of the repo tests.
func newUserConsentsRepoMock(t *testing.T) (pgxmock.PgxPoolIface, *UserConsentsRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewUserConsentsRepository(mock)
}

// TestUpsertConsent_InsertsNewRow asserts UpsertConsent fires the
// expected INSERT ... ON CONFLICT statement with all five forensic
// fields bound.
func TestUpsertConsent_InsertsNewRow(t *testing.T) {
	mock, repo := newUserConsentsRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	in := UpsertConsentInput{
		UserID:        userID,
		Purpose:       "tos",
		PolicyVersion: "v1.0",
		PolicySHA256:  "abc123",
		IP:            "1.2.3.4",
		UserAgent:     "UA-test",
	}

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO user_consents`).
		WithArgs(userID, "tos", "v1.0", "abc123", "1.2.3.4", "UA-test").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.UpsertConsent(ctx, tx, in))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUpsertConsent_OnConflictBumpsVersion asserts that a second
// UpsertConsent with the same (user, purpose) produces the same SQL —
// the ON CONFLICT branch is the same statement (pgxmock can't observe
// pg-side conflict resolution, but it CAN observe that the SQL contains
// the upsert idiom).
func TestUpsertConsent_OnConflictBumpsVersion(t *testing.T) {
	mock, repo := newUserConsentsRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	in := UpsertConsentInput{
		UserID:        userID,
		Purpose:       "tos",
		PolicyVersion: "v1.1",
		PolicySHA256:  "def456",
		IP:            "1.2.3.4",
		UserAgent:     "UA-test",
	}

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO user_consents .* ON CONFLICT \(user_id, purpose\) DO UPDATE`).
		WithArgs(userID, "tos", "v1.1", "def456", "1.2.3.4", "UA-test").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.UpsertConsent(ctx, tx, in))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestMarkWithdrawn_IdempotentOnAlreadyWithdrawn asserts the UPDATE
// includes the `withdrawn_at IS NULL` guard, making the second call a
// no-op without an error.
func TestMarkWithdrawn_IdempotentOnAlreadyWithdrawn(t *testing.T) {
	mock, repo := newUserConsentsRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()

	mock.ExpectBegin()
	// First call: row updated.
	mock.ExpectExec(`UPDATE user_consents.*withdrawn_at IS NULL`).
		WithArgs(userID, "pdn").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// Second call: zero rows updated (the guard kicked in).
	mock.ExpectExec(`UPDATE user_consents.*withdrawn_at IS NULL`).
		WithArgs(userID, "pdn").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectCommit()

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.MarkWithdrawn(ctx, tx, userID, "pdn"))
	require.NoError(t, repo.MarkWithdrawn(ctx, tx, userID, "pdn"))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestListByUser_ReturnsAllPurposes asserts the SELECT returns all
// three purpose rows sorted by purpose. pgxmock-backed: the order is
// implicit (the SQL has ORDER BY purpose, which pgxmock returns
// verbatim — we just need to confirm the scan parses the columns).
func TestListByUser_ReturnsAllPurposes(t *testing.T) {
	mock, repo := newUserConsentsRepoMock(t)
	ctx := context.Background()
	userID := uuid.New()
	now := time.Now().UTC()

	rows := pgxmock.NewRows([]string{"user_id", "purpose", "policy_version", "policy_sha256", "accepted_at", "withdrawn_at", "ip", "user_agent"}).
		AddRow(userID, "pdn", "v1.0", "sha-pdn", now, (*time.Time)(nil), "1.2.3.4", "UA").
		AddRow(userID, "privacy", "v1.0", "sha-priv", now, (*time.Time)(nil), "1.2.3.4", "UA").
		AddRow(userID, "tos", "v1.0", "sha-tos", now, (*time.Time)(nil), "1.2.3.4", "UA")

	mock.ExpectQuery(`SELECT user_id,\s+purpose`).
		WithArgs(userID).
		WillReturnRows(rows)

	got, err := repo.ListByUser(ctx, userID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "pdn", got[0].Purpose)
	require.Equal(t, "privacy", got[1].Purpose)
	require.Equal(t, "tos", got[2].Purpose)
	for _, c := range got {
		require.Equal(t, "v1.0", c.PolicyVersion)
		require.Nil(t, c.WithdrawnAt)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
