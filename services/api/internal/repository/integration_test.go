package repository

import (
	"context"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestIntegrationRepo(t *testing.T) (*integrationRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := &integrationRepository{
		pool: mockPool,
		sb:   newStatementBuilder(),
	}
	return repo, mockPool
}

func TestListByBusinessAndPlatform(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	platform := "vk"

	id1 := uuid.New()
	id2 := uuid.New()
	extID1 := "vk_account_111"
	extID2 := "vk_account_222"
	now := time.Now()

	repo, mockPool := newTestIntegrationRepo(t)

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "platform", "status",
		"encrypted_access_token", "encrypted_refresh_token",
		"external_id", "metadata", "token_expires_at",
		"created_at", "updated_at",
	}).
		AddRow(id1, businessID, platform, "active",
			[]byte("tok1"), []byte(nil),
			extID1, map[string]interface{}{}, &now,
			now, now).
		AddRow(id2, businessID, platform, "active",
			[]byte("tok2"), []byte(nil),
			extID2, map[string]interface{}{}, &now,
			now, now)

	// squirrel Eq map sorts keys alphabetically: business_id, platform
	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.ListByBusinessAndPlatform(ctx, businessID, platform)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, extID1, result[0].ExternalID)
	assert.Equal(t, extID2, result[1].ExternalID)
	assert.Equal(t, businessID, result[0].BusinessID)
	assert.Equal(t, platform, result[0].Platform)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestGetByBusinessPlatformExternal_Found(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	platform := "telegram"
	externalID := "tg_channel_999"
	integrationID := uuid.New()
	now := time.Now()

	repo, mockPool := newTestIntegrationRepo(t)

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "platform", "status",
		"encrypted_access_token", "encrypted_refresh_token",
		"external_id", "metadata", "token_expires_at",
		"created_at", "updated_at",
	}).
		AddRow(integrationID, businessID, platform, "active",
			[]byte("tok"), []byte(nil),
			externalID, map[string]interface{}{}, &now,
			now, now)

	// squirrel Eq map sorts keys alphabetically: business_id, external_id, platform
	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.GetByBusinessPlatformExternal(ctx, businessID, platform, externalID)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, integrationID, result.ID)
	assert.Equal(t, businessID, result.BusinessID)
	assert.Equal(t, platform, result.Platform)
	assert.Equal(t, externalID, result.ExternalID)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestGetByBusinessPlatformExternal_NotFound(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	platform := "telegram"
	externalID := "nonexistent_channel"

	repo, mockPool := newTestIntegrationRepo(t)

	// squirrel Eq map sorts keys alphabetically: business_id, external_id, platform
	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetByBusinessPlatformExternal(ctx, businessID, platform, externalID)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)

	require.NoError(t, mockPool.ExpectationsWereMet())
}
