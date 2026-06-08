package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
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
		"encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token",
		"external_id", "metadata", "token_expires_at", "user_token_expires_at",
		"created_at", "updated_at",
		"wrapped_dek",
	}).
		AddRow(id1, businessID, platform, "active",
			[]byte("tok1"), []byte(nil), []byte(nil),
			extID1, map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte("dek1")).
		AddRow(id2, businessID, platform, "active",
			[]byte("tok2"), []byte(nil), []byte(nil),
			extID2, map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte(nil))

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
		"encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token",
		"external_id", "metadata", "token_expires_at", "user_token_expires_at",
		"created_at", "updated_at",
		"wrapped_dek",
	}).
		AddRow(integrationID, businessID, platform, "active",
			[]byte("tok"), []byte(nil), []byte(nil),
			externalID, map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte("dek"))

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
	// WrappedDEK must round-trip out of the read path — otherwise DecryptToken
	// gets a nil DEK and falls back to the legacy key, failing to decrypt.
	assert.Equal(t, []byte("dek"), result.WrappedDEK)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestGetByBusinessPlatformExternal_NotFound(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	platform := "telegram"
	externalID := "nonexistent_channel"

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetByBusinessPlatformExternal(ctx, businessID, platform, externalID)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_GetByID_SoftDeleted(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE deleted_at IS NULL AND id = \$1`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetByID(ctx, id)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_SoftDelete_Success(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET deleted_at = \$1, updated_at = \$2 WHERE \(id = \$3 AND deleted_at IS NULL\)`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.SoftDelete(ctx, id)
	require.NoError(t, err)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_SoftDelete_AlreadyDeletedIsNoOp(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.SoftDelete(ctx, id)
	assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_SoftDelete_NotFound(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.SoftDelete(ctx, id)
	assert.ErrorIs(t, err, domain.ErrIntegrationNotFound)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_DeleteOlderThan(t *testing.T) {
	ctx := context.Background()
	cutoff := time.Now().Add(-90 * 24 * time.Hour)

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`DELETE FROM integrations WHERE \(deleted_at IS NOT NULL AND deleted_at < \$1\)`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 3))

	n, err := repo.DeleteOlderThan(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_ListByBusinessID_ExcludesSoftDeleted(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	id1 := uuid.New()
	now := time.Now()

	repo, mockPool := newTestIntegrationRepo(t)

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "platform", "status",
		"encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token",
		"external_id", "metadata", "token_expires_at", "user_token_expires_at",
		"created_at", "updated_at",
		"wrapped_dek",
	}).
		AddRow(id1, businessID, "vk", "active",
			[]byte("tok"), []byte(nil), []byte(nil),
			"vk_111", map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte(nil))

	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE deleted_at IS NULL AND business_id = \$1`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.ListByBusinessID(ctx, businessID)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, id1, result[0].ID)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_ListAllActiveByPlatforms_ExcludesSoftDeleted(t *testing.T) {
	ctx := context.Background()
	id1 := uuid.New()
	businessID := uuid.New()
	now := time.Now()

	repo, mockPool := newTestIntegrationRepo(t)

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "platform", "status",
		"encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token",
		"external_id", "metadata", "token_expires_at", "user_token_expires_at",
		"created_at", "updated_at",
		"wrapped_dek",
	}).
		AddRow(id1, businessID, "telegram", "active",
			[]byte("tok"), []byte(nil), []byte(nil),
			"tg_999", map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte(nil))

	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE deleted_at IS NULL AND status = \$1 AND platform IN`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.ListAllActiveByPlatforms(ctx, []string{"telegram", "vk"})
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, id1, result[0].ID)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_Create_PersistsEnvelopeColumns guards the Phase 30
// regression where Create dropped wrapped_dek/key_version/
// encryption_key_fingerprint, leaving every new integration's token
// unrecoverable (encrypted under an envelope DEK the row never stored).
func TestIntegrationRepo_Create_PersistsEnvelopeColumns(t *testing.T) {
	ctx := context.Background()
	repo, mockPool := newTestIntegrationRepo(t)

	integration := &domain.Integration{
		ID:                       uuid.New(),
		BusinessID:               uuid.New(),
		Platform:                 "telegram",
		Status:                   "active",
		EncryptedAccessToken:     []byte("ct"),
		ExternalID:               "tg_1",
		Metadata:                 map[string]interface{}{},
		WrappedDEK:               []byte("wrapped-dek"),
		KeyVersion:               7,
		EncryptionKeyFingerprint: "fp-xyz",
	}

	mockPool.ExpectExec(`INSERT INTO integrations.*wrapped_dek.*key_version.*encryption_key_fingerprint`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			[]byte("wrapped-dek"), int16(7), "fp-xyz",
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := repo.Create(ctx, integration)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_Update_PersistsEnvelopeColumns guards the matching gap
// on the token-refresh path: Update must re-persist the freshly wrapped DEK,
// otherwise a rotation re-orphans the token on the very next read.
func TestIntegrationRepo_Update_PersistsEnvelopeColumns(t *testing.T) {
	ctx := context.Background()
	repo, mockPool := newTestIntegrationRepo(t)

	integration := &domain.Integration{
		ID:                       uuid.New(),
		Status:                   "active",
		EncryptedAccessToken:     []byte("ct"),
		ExternalID:               "tg_1",
		Metadata:                 map[string]interface{}{},
		WrappedDEK:               []byte("new-dek"),
		KeyVersion:               9,
		EncryptionKeyFingerprint: "fp-new",
	}

	mockPool.ExpectExec(`UPDATE integrations SET.*wrapped_dek.*key_version.*encryption_key_fingerprint`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			[]byte("new-dek"), int16(9), "fp-new",
			pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, integration)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}
