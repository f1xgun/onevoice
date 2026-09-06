//go:build integration

package authz_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestEnsureOwnerExistsAfterDeletionPredicate(t *testing.T) {
	dsn := os.Getenv("AUTHZ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AUTHZ_TEST_DATABASE_URL is required for PostgreSQL integration coverage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	now := time.Now().UTC()
	for _, tc := range []struct {
		name                string
		requested, canceled *time.Time
		blocked             bool
	}{
		{"live", nil, nil, false},
		{"pending deletion", &now, nil, true},
		{"canceled deletion", &now, &now, false},
		{"cancellation without request", nil, &now, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			require.NoError(t, err)
			defer func() { require.NoError(t, tx.Rollback(ctx)) }()
			_, err = tx.Exec(ctx, `CREATE TEMP TABLE users (id uuid PRIMARY KEY, deletion_requested_at timestamptz, deletion_canceled_at timestamptz) ON COMMIT DROP;
CREATE TEMP TABLE business_members (business_id uuid, user_id uuid, role_id uuid, status text) ON COMMIT DROP`)
			require.NoError(t, err)
			actor, coOwner, businessID := uuid.New(), uuid.New(), uuid.New()
			_, err = tx.Exec(ctx, `INSERT INTO pg_temp.users VALUES ($1, NULL, NULL), ($2, $3, $4)`, actor, coOwner, tc.requested, tc.canceled)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO pg_temp.business_members VALUES ($1, $2, $4, 'active'), ($1, $3, $4, 'active')`, businessID, actor, coOwner, uuid.MustParse(domain.SystemRoleOwnerID))
			require.NoError(t, err)
			for _, kind := range []authz.OwnerChangeKind{authz.OwnerChangeRemove, authz.OwnerChangeDemote} {
				err = authz.EnsureOwnerExistsAfter(ctx, tx, businessID, authz.OwnerChange{Kind: kind, MemberUserID: &actor})
				if tc.blocked {
					require.ErrorIs(t, err, authz.ErrLastOwner)
				} else {
					require.NoError(t, err)
				}
			}
		})
	}
}
