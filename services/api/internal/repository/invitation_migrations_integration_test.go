//go:build integration

package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestInvitationMigrationsRollback(t *testing.T) {
	dsn := os.Getenv("MIGRATION_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("MIGRATION_TEST_POSTGRES_URL must point to an empty disposable database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(ctx)) }()
	for _, directory := range []string{"../../../../migrations/postgres", "../../migrations"} {
		t.Run(directory, func(t *testing.T) {
			var count int
			require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'").Scan(&count))
			require.Zero(t, count, "migration test requires an empty database")
			up, err := filepath.Glob(filepath.Join(directory, "*.up.sql"))
			require.NoError(t, err)
			require.NotEmpty(t, up)
			for _, path := range up {
				applyInvitationMigration(t, ctx, conn, path)
			}
			userID, businessID := uuid.New(), uuid.New()
			_, err = conn.Exec(ctx, "INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)", userID, "migration@example.test", "unused")
			require.NoError(t, err)
			_, err = conn.Exec(ctx, "INSERT INTO businesses (id, name) VALUES ($1, $2)", businessID, "Migration test")
			require.NoError(t, err)
			_, err = conn.Exec(ctx, "INSERT INTO business_members (business_id, user_id, role_id) VALUES ($1, $2, $3)", businessID, userID, "00000000-0000-0000-0000-000000000001")
			require.NoError(t, err)
			for _, status := range []string{"accepted", "revoked", "expired"} {
				t.Run(status, func(t *testing.T) {
					roleID, invitationID := uuid.New(), uuid.New()
					_, err := conn.Exec(ctx, "INSERT INTO roles (id, business_id, name) VALUES ($1, $2, $3)", roleID, businessID, status)
					require.NoError(t, err)
					_, err = conn.Exec(ctx, `INSERT INTO invitations
      (id, business_id, role_id, token_hash, created_by, expires_at, accepted_at, revoked_at)
      VALUES ($1, $2, $3, $4, $5,
       CASE WHEN $6 = 'expired' THEN now() - interval '1 day' ELSE now() + interval '1 day' END,
       CASE WHEN $6 = 'accepted' THEN now() END,
       CASE WHEN $6 = 'revoked' THEN now() END)`, invitationID, businessID, roleID, invitationID.String(), userID, status)
					require.NoError(t, err)
					tx, err := conn.Begin(ctx)
					require.NoError(t, err)
					defer func() { _ = tx.Rollback(ctx) }()
					require.NoError(t, NewRoleRepository(nil).DeleteInTx(ctx, tx, roleID))
					require.NoError(t, tx.Commit(ctx))
					var detached bool
					require.NoError(t, conn.QueryRow(ctx, "SELECT role_id IS NULL FROM invitations WHERE id = $1", invitationID).Scan(&detached))
					require.True(t, detached)
					require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM roles WHERE id = $1", roleID).Scan(&count))
					require.Zero(t, count)
				})
			}
			down, err := filepath.Glob(filepath.Join(directory, "*.down.sql"))
			require.NoError(t, err)
			require.Len(t, down, len(up))
			for i := len(down) - 1; i >= 0; i-- {
				applyInvitationMigration(t, ctx, conn, down[i])
				if strings.HasSuffix(down[i], "_terminal_invitation_roles.down.sql") {
					require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM invitations WHERE role_id IS NULL").Scan(&count))
					require.Equal(t, 3, count)
					var nullable string
					require.NoError(t, conn.QueryRow(ctx, "SELECT is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'invitations' AND column_name = 'role_id'").Scan(&nullable))
					require.Equal(t, "YES", nullable)
				}
			}
			require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'").Scan(&count))
			require.Zero(t, count)
			t.Logf("applied %d up and %d down migrations; detached invitations survived role rollback", len(up), len(down))
		})
	}
}

func applyInvitationMigration(t *testing.T, ctx context.Context, conn *pgx.Conn, path string) {
	t.Helper()
	sql, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	_, err = conn.Exec(ctx, string(sql))
	require.NoError(t, err, path)
}
