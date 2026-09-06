package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestTokenConsumptionRejectsChangedAddress(t *testing.T) {
	for _, table := range []string{"password_reset_tokens", "email_verification_tokens"} {
		t.Run(table, func(t *testing.T) {
			pool, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer pool.Close()
			pool.ExpectBegin()
			tx, err := pool.Begin(context.Background())
			require.NoError(t, err)
			pool.ExpectQuery(`WITH locked_user AS .*SELECT u.id, u.email FROM users u.*JOIN ` + table + ` t ON t.user_id = u.id.*FOR UPDATE OF u.*UPDATE ` + table + ` t.*FROM locked_user u.*t.user_id = u.id.*t.email = u.email`).
				WithArgs([]byte("old-mailbox-link-hash")).
				WillReturnError(pgx.ErrNoRows)
			if table == "password_reset_tokens" {
				id, consumeErr := NewPasswordResetTokenRepository(pool).ConsumeAtomic(context.Background(), tx, []byte("old-mailbox-link-hash"))
				require.ErrorIs(t, consumeErr, domain.ErrResetTokenInvalid)
				require.Equal(t, uuid.Nil, id)
			} else {
				id, email, consumeErr := NewEmailVerificationTokenRepository(pool).ConsumeAtomic(context.Background(), tx, []byte("old-mailbox-link-hash"))
				require.ErrorIs(t, consumeErr, domain.ErrVerifyTokenInvalid)
				require.Equal(t, uuid.Nil, id)
				require.Empty(t, email)
			}
			require.NoError(t, pool.ExpectationsWereMet())
		})
	}
}
