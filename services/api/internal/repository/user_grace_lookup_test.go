package repository

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// graceLookupRepo opens a pgxpool against TEST_POSTGRES_URL, creating an
// isolated per-test schema that owns a minimal users table mirroring the
// columns the Get paths select. The schema is dropped on cleanup and the
// search_path is pinned so the repository's unqualified `users` references
// resolve there.
func graceLookupRepo(t *testing.T) (context.Context, *pgxpool.Pool, *userRepository) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping user grace-window lookup test")
	}

	ctx := context.Background()
	schema := "user_grace_lookup_" + uuid.NewString()[:8]

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
		pool.Close()
	})

	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s.users (
		id uuid PRIMARY KEY,
		email text NOT NULL,
		name text NOT NULL DEFAULT '',
		password_hash text NOT NULL DEFAULT '',
		preferred_locale text NOT NULL DEFAULT 'ru',
		email_verified boolean NOT NULL DEFAULT false,
		email_verified_at timestamptz,
		deleted_at timestamptz,
		deletion_requested_at timestamptz,
		deletion_canceled_at timestamptz,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now()
	)`, schema))
	require.NoError(t, err)

	repo := &userRepository{pool: pool, sb: newStatementBuilder()}
	return ctx, pool, repo
}

// TestUserRepository_GraceWindowLookupSemantics is the fail-on-revert guard for
// the connect-actor gate's pending-deletion half. RequestDeletion stamps both
// deletion_requested_at AND deleted_at, so a user inside the grace window is
// soft-deleted. The gate must read such a user to reject the public OAuth
// callback persisting a live integration mid-deletion; the `deleted_at IS NULL`
// filtered GetByID misses it (fail-open), while GetByIDIncludingDeleted finds
// it. This test pins both lookups against the real SQL so reverting the gate to
// GetByID is caught here: GetByID returns ErrUserNotFound for the grace-window
// row, which would let the gate fall through and the integration persist.
func TestUserRepository_GraceWindowLookupSemantics(t *testing.T) {
	ctx, pool, repo := graceLookupRepo(t)

	graceID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO users
		(id, email, email_verified, deleted_at, deletion_requested_at, deletion_canceled_at)
		VALUES ($1, $2, TRUE, NOW(), NOW(), NULL)`,
		graceID, "grace@example.com")
	require.NoError(t, err)

	t.Run("GetByID misses the grace-window (soft-deleted) row", func(t *testing.T) {
		_, err := repo.GetByID(ctx, graceID)
		require.ErrorIs(t, err, domain.ErrUserNotFound,
			"deleted_at-filtered GetByID must NOT see a grace-window user — reverting the gate to this lookup fails open")
	})

	t.Run("GetByIDIncludingDeleted finds the grace-window row with deletion fields", func(t *testing.T) {
		u, err := repo.GetByIDIncludingDeleted(ctx, graceID)
		require.NoError(t, err)
		require.NotNil(t, u)
		assert.True(t, u.EmailVerified)
		require.NotNil(t, u.DeletedAt, "the row is soft-deleted")
		require.NotNil(t, u.DeletionRequestedAt, "the gate needs deletion_requested_at to reject")
		assert.Nil(t, u.DeletionCanceledAt, "an uncanceled request is genuinely pending")
	})
}
