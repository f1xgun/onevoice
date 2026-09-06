package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// newInvitationRepoMock returns a fresh mock pool + repo (interface-bound).
// squirrel passes uuid.UUID through driver.Valuer → string, so UUID args
// are matched with pgxmock.AnyArg() (mirrors business_member_test.go).
func newInvitationRepoMock(t *testing.T) (pgxmock.PgxPoolIface, domain.InvitationRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, NewInvitationRepository(mock)
}

func TestInvitationRepo_GetByTokenHash_Found(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()

	id := uuid.New()
	bizID := uuid.New()
	roleID := uuid.New()
	createdBy := uuid.New()
	now := time.Now().UTC()
	hash := "abc123"

	rows := mock.NewRows([]string{
		"id", "business_id", "role_id", "token_hash", "expires_at",
		"accepted_at", "accepted_by", "revoked_at", "created_by", "created_at",
	}).AddRow(id, bizID, roleID, hash, now.Add(time.Hour),
		(*time.Time)(nil), (*uuid.UUID)(nil), (*time.Time)(nil), createdBy, now)

	mock.ExpectQuery(`SELECT .+ FROM invitations WHERE token_hash`).
		WithArgs(hash).
		WillReturnRows(rows)

	inv, err := repo.GetByTokenHash(ctx, hash)
	require.NoError(t, err)
	require.NotNil(t, inv)
	require.Equal(t, id, inv.ID)
	require.Equal(t, hash, inv.TokenHash)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_GetByTokenHash_NotFound(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	hash := "missing"

	mock.ExpectQuery(`SELECT .+ FROM invitations WHERE token_hash`).
		WithArgs(hash).
		WillReturnError(pgx.ErrNoRows)

	_, err := repo.GetByTokenHash(ctx, hash)
	require.Error(t, err)
	require.True(t, errors.Is(err, domain.ErrInvitationNotFound),
		"expected ErrInvitationNotFound, got %v", err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_CreateInTx_HappyPath(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()

	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.Serializable})
	mock.ExpectExec(`INSERT INTO invitations`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	tx, err := mock.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	require.NoError(t, err)

	inv := &domain.Invitation{
		BusinessID: uuid.New(),
		RoleID:     uuid.New(),
		TokenHash:  "h",
		ExpiresAt:  time.Now().Add(time.Hour).UTC(),
		CreatedBy:  uuid.New(),
	}
	require.NoError(t, repo.CreateInTx(ctx, tx, inv))
	require.NoError(t, tx.Commit(ctx))
	require.NotEqual(t, uuid.Nil, inv.ID, "Create should populate ID when zero")
	require.False(t, inv.CreatedAt.IsZero(), "Create should populate CreatedAt when zero")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_CreateInTx_NilTx(t *testing.T) {
	_, repo := newInvitationRepoMock(t)
	err := repo.CreateInTx(context.Background(), nil, &domain.Invitation{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "tx is required")
}

func TestInvitationRepo_CreateInTx_TokenHashCollision(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()

	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.Serializable})
	mock.ExpectExec(`INSERT INTO invitations`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"})

	tx, err := mock.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	require.NoError(t, err)

	inv := &domain.Invitation{
		BusinessID: uuid.New(),
		RoleID:     uuid.New(),
		TokenHash:  "h",
		ExpiresAt:  time.Now().Add(time.Hour).UTC(),
		CreatedBy:  uuid.New(),
	}
	err = repo.CreateInTx(ctx, tx, inv)
	require.Error(t, err)
	require.Contains(t, err.Error(), "token_hash unique violation")
}

func TestInvitationRepo_CountPendingByBusinessInTx(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	bizID := uuid.New()

	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.Serializable})
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM invitations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(mock.NewRows([]string{"count"}).AddRow(5))

	tx, err := mock.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	require.NoError(t, err)

	count, err := repo.CountPendingByBusinessInTx(ctx, tx, bizID)
	require.NoError(t, err)
	require.Equal(t, 5, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_ListPendingByBusiness(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	bizID := uuid.New()

	now := time.Now().UTC()
	rows := mock.NewRows([]string{
		"id", "business_id", "role_id", "token_hash", "expires_at",
		"accepted_at", "accepted_by", "revoked_at", "created_by", "created_at",
	}).
		AddRow(uuid.New(), bizID, uuid.New(), "h1", now.Add(time.Hour),
			(*time.Time)(nil), (*uuid.UUID)(nil), (*time.Time)(nil), uuid.New(), now).
		AddRow(uuid.New(), bizID, uuid.New(), "h2", now.Add(2*time.Hour),
			(*time.Time)(nil), (*uuid.UUID)(nil), (*time.Time)(nil), uuid.New(), now)

	mock.ExpectQuery(`SELECT .+ FROM invitations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	out, err := repo.ListPendingByBusiness(ctx, bizID)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_Revoke_Wins(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	id := uuid.New()
	bizID := uuid.New()

	mock.ExpectExec(`UPDATE invitations SET revoked_at`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.Revoke(ctx, id, bizID))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_Revoke_CrossTenant_NotFound(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	id := uuid.New()
	bizID := uuid.New()

	mock.ExpectExec(`UPDATE invitations SET revoked_at`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery(`SELECT accepted_at, revoked_at, expires_at FROM invitations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	err := repo.Revoke(ctx, id, bizID)
	require.Error(t, err)
	require.True(t, errors.Is(err, domain.ErrInvitationNotFound))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_Revoke_AlreadyRevoked_StateError(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	id := uuid.New()
	bizID := uuid.New()
	revokedAt := time.Now().UTC()

	mock.ExpectExec(`UPDATE invitations SET revoked_at`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery(`SELECT accepted_at, revoked_at, expires_at FROM invitations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(mock.NewRows([]string{"accepted_at", "revoked_at", "expires_at"}).
			AddRow((*time.Time)(nil), &revokedAt, time.Now().Add(time.Hour).UTC()))

	err := repo.Revoke(ctx, id, bizID)
	require.Error(t, err)
	require.True(t, errors.Is(err, domain.ErrInvitationRevoked))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_MarkAcceptedInTx_Wins(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	id := uuid.New()
	userID := uuid.New()

	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mock.ExpectExec(`UPDATE invitations SET accepted_at`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), domain.SystemRoleOwnerID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	tx, err := mock.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)

	require.NoError(t, repo.MarkAcceptedInTx(ctx, tx, id, userID))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_MarkAcceptedInTx_LosesRace_AlreadyAccepted(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	id := uuid.New()
	userID := uuid.New()
	acceptedAt := time.Now().UTC()

	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mock.ExpectExec(`UPDATE invitations SET accepted_at`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), domain.SystemRoleOwnerID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery(`SELECT accepted_at, revoked_at, expires_at FROM invitations WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(mock.NewRows([]string{"accepted_at", "revoked_at", "expires_at"}).
			AddRow(&acceptedAt, (*time.Time)(nil), time.Now().Add(time.Hour).UTC()))

	tx, err := mock.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)

	err = repo.MarkAcceptedInTx(ctx, tx, id, userID)
	require.Error(t, err)
	require.True(t, errors.Is(err, domain.ErrInvitationAccepted))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvitationRepo_MarkAcceptedInTx_LosesRace_Expired(t *testing.T) {
	mock, repo := newInvitationRepoMock(t)
	ctx := context.Background()
	id := uuid.New()
	userID := uuid.New()
	expiredAt := time.Now().Add(-time.Hour).UTC()

	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	mock.ExpectExec(`UPDATE invitations SET accepted_at`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), domain.SystemRoleOwnerID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery(`SELECT accepted_at, revoked_at, expires_at FROM invitations WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(mock.NewRows([]string{"accepted_at", "revoked_at", "expires_at"}).
			AddRow((*time.Time)(nil), (*time.Time)(nil), expiredAt))

	tx, err := mock.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)

	err = repo.MarkAcceptedInTx(ctx, tx, id, userID)
	require.Error(t, err)
	require.True(t, errors.Is(err, domain.ErrInvitationExpired))
	require.NoError(t, mock.ExpectationsWereMet())
}

type delayedInvitationInsertTx struct {
	pgx.Tx
	started chan struct{}
	resume  chan struct{}
}

func (tx delayedInvitationInsertTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	close(tx.started)
	select {
	case <-tx.resume:
		return tx.Tx.Exec(ctx, sql, args...)
	case <-ctx.Done():
		return pgconn.CommandTag{}, ctx.Err()
	}
}

func TestInvitationRepo_CreateCommitsAfterRemovalCannotBeAccepted(t *testing.T) {
	for _, change := range []string{"removed", "demoted"} {
		t.Run(change, func(t *testing.T) {
			pool, repo := newInvitationRepoMock(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			inv := &domain.Invitation{ID: uuid.New(), BusinessID: uuid.New(), CreatedBy: uuid.New(), RoleID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)}
			pool.ExpectBegin()
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			delayed := delayedInvitationInsertTx{Tx: tx, started: make(chan struct{}), resume: make(chan struct{})}
			result := make(chan error, 1)
			go func() { result <- repo.CreateInTx(ctx, delayed, inv) }()
			<-delayed.started

			removalPool, removalRepo := newInvitationRepoMock(t)
			removalPool.ExpectBegin()
			removalTx, err := removalPool.Begin(ctx)
			require.NoError(t, err)
			members := NewBusinessMembershipRepository(removalPool)
			if change == "removed" {
				removalPool.ExpectExec("DELETE FROM business_members").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("DELETE", 1))
				require.NoError(t, members.DeleteInTx(ctx, removalTx, inv.BusinessID, inv.CreatedBy))
			} else {
				removalPool.ExpectExec("UPDATE business_members SET").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
				require.NoError(t, members.UpdateRoleInTx(ctx, removalTx, inv.BusinessID, inv.CreatedBy, uuid.New(), uuid.New()))
			}
			removalPool.ExpectExec("UPDATE invitations SET revoked_at").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
				WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			count, err := removalRepo.RevokeByCreatorInTx(ctx, removalTx, inv.BusinessID, inv.CreatedBy)
			require.NoError(t, err)
			require.Zero(t, count)
			removalPool.ExpectCommit()
			require.NoError(t, removalTx.Commit(ctx))
			require.NoError(t, removalPool.ExpectationsWereMet())
			pool.ExpectExec("INSERT INTO invitations").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
			close(delayed.resume)
			require.NoError(t, <-result)
			pool.ExpectCommit()
			require.NoError(t, tx.Commit(ctx))
			pool.ExpectBegin()
			tx, err = pool.Begin(ctx)
			require.NoError(t, err)

			pool.ExpectExec(`UPDATE invitations SET accepted_at.*EXISTS.*m.business_id = invitations.business_id.*m.user_id = invitations.created_by.*m.status = 'active'.*creator_role.permissions @> '\["members.invite"\]'::jsonb.*m.role_id = \$5 OR creator_role.permissions @> invited_role.permissions.*FOR SHARE OF m, creator_role, invited_role`).
				WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), domain.SystemRoleOwnerID).
				WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			pool.ExpectQuery("SELECT accepted_at, revoked_at, expires_at FROM invitations").WithArgs(inv.ID.String()).
				WillReturnRows(pool.NewRows([]string{"accepted_at", "revoked_at", "expires_at"}).
					AddRow(nil, nil, inv.ExpiresAt))
			require.ErrorIs(t, repo.MarkAcceptedInTx(ctx, tx, inv.ID, uuid.New()), domain.ErrInvitationRevoked)
			require.NoError(t, pool.ExpectationsWereMet())
		})
	}
}

func TestInvitationRepo_DetachedTerminalRole(t *testing.T) {
	for _, state := range []string{"accepted", "revoked", "expired"} {
		t.Run(state, func(t *testing.T) {
			pool, repo := newInvitationRepoMock(t)
			now := time.Now().UTC()
			expiry := now.Add(time.Hour)
			var accepted, revoked *time.Time
			switch state {
			case "accepted":
				accepted = &now
			case "revoked":
				revoked = &now
			case "expired":
				expiry = now.Add(-time.Hour)
			}
			pool.ExpectQuery(`SELECT .+ FROM invitations WHERE token_hash`).WithArgs("hash").WillReturnRows(pool.NewRows([]string{
				"id", "business_id", "role_id", "token_hash", "expires_at", "accepted_at", "accepted_by", "revoked_at", "created_by", "created_at",
			}).AddRow(uuid.New(), uuid.New(), nil, "hash", expiry, accepted, (*uuid.UUID)(nil), revoked, uuid.New(), now))
			inv, err := repo.GetByTokenHash(context.Background(), "hash")
			require.NoError(t, err)
			require.Equal(t, uuid.Nil, inv.RoleID)
			require.Equal(t, accepted, inv.AcceptedAt)
			require.Equal(t, revoked, inv.RevokedAt)
			require.Equal(t, expiry, inv.ExpiresAt)
			require.NoError(t, pool.ExpectationsWereMet())
		})
	}
}
