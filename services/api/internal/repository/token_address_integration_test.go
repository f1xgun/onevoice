//go:build integration

package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestTokenAddressBindingPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(ctx)) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE users (id UUID PRIMARY KEY, email TEXT NOT NULL);
		CREATE TEMP TABLE password_reset_tokens (
			user_id UUID, email TEXT NOT NULL, token_hash BYTEA UNIQUE,
			expires_at TIMESTAMPTZ, consumed_at TIMESTAMPTZ
		);
		CREATE TEMP TABLE email_verification_tokens (
			user_id UUID, email TEXT NOT NULL, token_hash BYTEA UNIQUE,
			expires_at TIMESTAMPTZ, consumed_at TIMESTAMPTZ
		);`)
	require.NoError(t, err)
	id := uuid.New()
	_, err = tx.Exec(ctx, "INSERT INTO users VALUES ($1, $2)", id, "old@example.test")
	require.NoError(t, err)
	reset := NewPasswordResetTokenRepository(nil)
	verify := NewEmailVerificationTokenRepository(nil)
	expiry := time.Now().Add(time.Hour)
	require.NoError(t, reset.Insert(ctx, tx, id, "old@example.test", []byte("old-reset"), expiry))
	require.NoError(t, verify.Insert(ctx, tx, id, "old@example.test", []byte("old-verify"), expiry))
	_, err = tx.Exec(ctx, "UPDATE users SET email = $1 WHERE id = $2", "new@example.test", id)
	require.NoError(t, err)

	_, err = reset.ConsumeAtomic(ctx, tx, []byte("old-reset"))
	require.ErrorIs(t, err, domain.ErrResetTokenInvalid)
	_, _, err = verify.ConsumeAtomic(ctx, tx, []byte("old-verify"))
	require.ErrorIs(t, err, domain.ErrVerifyTokenInvalid)

	require.NoError(t, reset.Insert(ctx, tx, id, "new@example.test", []byte("new-reset"), expiry))
	require.NoError(t, verify.Insert(ctx, tx, id, "new@example.test", []byte("new-verify"), expiry))
	gotID, err := reset.ConsumeAtomic(ctx, tx, []byte("new-reset"))
	require.NoError(t, err)
	require.Equal(t, id, gotID)
	gotID, email, err := verify.ConsumeAtomic(ctx, tx, []byte("new-verify"))
	require.NoError(t, err)
	require.Equal(t, id, gotID)
	require.Equal(t, "new@example.test", email)
	_, err = reset.ConsumeAtomic(ctx, tx, []byte("new-reset"))
	require.ErrorIs(t, err, domain.ErrResetTokenInvalid)
	_, _, err = verify.ConsumeAtomic(ctx, tx, []byte("new-verify"))
	require.ErrorIs(t, err, domain.ErrVerifyTokenInvalid)
}
