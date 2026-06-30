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

// ptrInt16/ptrString back the nullable key_version/encryption_key_fingerprint
// mock columns: pgxmock scans those into *int16/*string, so a non-NULL AddRow
// value must itself be a pointer (a bare int16/string is rejected as a kind
// mismatch); a typed nil pointer models the NULL legacy/dual-read case.
func ptrInt16(v int16) *int16    { return &v }
func ptrString(v string) *string { return &v }

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
		"wrapped_dek", "key_version", "encryption_key_fingerprint",
	}).
		AddRow(id1, businessID, platform, "active",
			[]byte("tok1"), []byte(nil), []byte(nil),
			extID1, map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte("dek1"), ptrInt16(3), ptrString("fp1")).
		AddRow(id2, businessID, platform, "active",
			[]byte("tok2"), []byte(nil), []byte(nil),
			extID2, map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte(nil), (*int16)(nil), (*string)(nil))

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
		"wrapped_dek", "key_version", "encryption_key_fingerprint",
	}).
		AddRow(integrationID, businessID, platform, "active",
			[]byte("tok"), []byte(nil), []byte(nil),
			externalID, map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte("dek"), ptrInt16(5), ptrString("fp-found"))

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

func TestGetActiveByPlatformExternal_Found(t *testing.T) {
	ctx := context.Background()
	otherBusiness := uuid.New()
	platform := "telegram"
	externalID := "@victimshop"
	integrationID := uuid.New()
	now := time.Now()

	repo, mockPool := newTestIntegrationRepo(t)

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "platform", "status",
		"encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token",
		"external_id", "metadata", "token_expires_at", "user_token_expires_at",
		"created_at", "updated_at",
		"wrapped_dek", "key_version", "encryption_key_fingerprint",
	}).
		AddRow(integrationID, otherBusiness, platform, "active",
			[]byte("tok"), []byte(nil), []byte(nil),
			externalID, map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte("dek"), ptrInt16(1), ptrString("fp"))

	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.GetActiveByPlatformExternal(ctx, platform, externalID)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, otherBusiness, result.BusinessID)
	assert.Equal(t, externalID, result.ExternalID)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestGetActiveByPlatformExternal_NotFound(t *testing.T) {
	ctx := context.Background()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetActiveByPlatformExternal(ctx, "telegram", "@nobody")
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
		"wrapped_dek", "key_version", "encryption_key_fingerprint",
	}).
		AddRow(id1, businessID, "vk", "active",
			[]byte("tok"), []byte(nil), []byte(nil),
			"vk_111", map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte(nil), (*int16)(nil), (*string)(nil))

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
		"wrapped_dek", "key_version", "encryption_key_fingerprint",
	}).
		AddRow(id1, businessID, "telegram", "active",
			[]byte("tok"), []byte(nil), []byte(nil),
			"tg_999", map[string]interface{}{}, &now, (*time.Time)(nil),
			now, now, []byte(nil), (*int16)(nil), (*string)(nil))

	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE deleted_at IS NULL AND status = \$1 AND platform IN`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.ListAllActiveByPlatforms(ctx, []string{"telegram", "vk"})
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, id1, result[0].ID)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_MarkTokenExpired_FlipsActiveRows(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	platform := "telegram"

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET status = \$1, updated_at = \$2 WHERE business_id = \$3 AND deleted_at IS NULL AND platform = \$4 AND status = \$5`).
		WithArgs("token_expired", pgxmock.AnyArg(), pgxmock.AnyArg(), platform, "active").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	n, err := repo.MarkTokenExpired(ctx, businessID, platform, "")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_MarkTokenExpired_ScopedByExternalID — a non-empty
// external_id must narrow the WHERE so only the failing channel flips; a sibling
// channel of the same platform keeps its active status.
func TestIntegrationRepo_MarkTokenExpired_ScopedByExternalID(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	platform := "telegram"
	externalID := "@first_channel"

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET status = \$1, updated_at = \$2 WHERE business_id = \$3 AND deleted_at IS NULL AND external_id = \$4 AND platform = \$5 AND status = \$6`).
		WithArgs("token_expired", pgxmock.AnyArg(), pgxmock.AnyArg(), externalID, platform, "active").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	n, err := repo.MarkTokenExpired(ctx, businessID, platform, externalID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestIntegrationRepo_MarkTokenExpired_NoActiveRowsIsNoOp(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET status = \$1`).
		WithArgs("token_expired", pgxmock.AnyArg(), pgxmock.AnyArg(), "vk", "active").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	n, err := repo.MarkTokenExpired(ctx, businessID, "vk", "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_MarkTokenExpired_IDFormMismatchFallsBack covers the case
// where the LLM-supplied external_id differs in form from the stored value
// (e.g. VK "123" vs stored "-123"): the scoped UPDATE matches zero rows, the
// existence check finds NO row for that id, so the flip falls back to the
// platform-wide UPDATE and the genuinely-broken integration is still flipped.
func TestIntegrationRepo_MarkTokenExpired_IDFormMismatchFallsBack(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	platform := "vk"
	externalID := "123"

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET status = \$1, updated_at = \$2 WHERE business_id = \$3 AND deleted_at IS NULL AND external_id = \$4 AND platform = \$5 AND status = \$6`).
		WithArgs("token_expired", pgxmock.AnyArg(), pgxmock.AnyArg(), externalID, platform, "active").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	mockPool.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM integrations WHERE business_id = \$1 AND deleted_at IS NULL AND external_id = \$2 AND platform = \$3\)`).
		WithArgs(pgxmock.AnyArg(), externalID, platform).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))

	mockPool.ExpectExec(`UPDATE integrations SET status = \$1, updated_at = \$2 WHERE business_id = \$3 AND deleted_at IS NULL AND platform = \$4 AND status = \$5`).
		WithArgs("token_expired", pgxmock.AnyArg(), pgxmock.AnyArg(), platform, "active").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	n, err := repo.MarkTokenExpired(ctx, businessID, platform, externalID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_MarkTokenExpired_InactiveSiblingNoFallback covers the
// guard: the supplied external_id maps to a present-but-inactive integration
// (already expired/disconnected), so the scoped UPDATE matches zero active rows
// but the existence check finds the row. The flip returns 0 WITHOUT falling
// back, so a healthy sibling integration is never collateral-flipped.
func TestIntegrationRepo_MarkTokenExpired_InactiveSiblingNoFallback(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()
	platform := "telegram"
	externalID := "@already_expired"

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET status = \$1, updated_at = \$2 WHERE business_id = \$3 AND deleted_at IS NULL AND external_id = \$4 AND platform = \$5 AND status = \$6`).
		WithArgs("token_expired", pgxmock.AnyArg(), pgxmock.AnyArg(), externalID, platform, "active").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	mockPool.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM integrations WHERE business_id = \$1 AND deleted_at IS NULL AND external_id = \$2 AND platform = \$3\)`).
		WithArgs(pgxmock.AnyArg(), externalID, platform).
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

	n, err := repo.MarkTokenExpired(ctx, businessID, platform, externalID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)

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

// TestIntegrationRepo_Update_SoftDeletedReturnsNotFound asserts the WHERE guards
// on deleted_at IS NULL: a soft-deleted (or absent) row affects zero rows and
// surfaces ErrIntegrationNotFound instead of resurrecting the tombstoned row.
// The regexp pins the deleted_at IS NULL predicate, so dropping the guard fails
// this test.
func TestIntegrationRepo_Update_SoftDeletedReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	repo, mockPool := newTestIntegrationRepo(t)

	integration := &domain.Integration{
		ID:                   uuid.New(),
		Status:               "active",
		EncryptedAccessToken: []byte("ct"),
		ExternalID:           "tg_1",
		Metadata:             map[string]interface{}{},
	}

	mockPool.ExpectExec(`UPDATE integrations SET.*WHERE.*deleted_at IS NULL`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Update(ctx, integration)
	require.ErrorIs(t, err, domain.ErrIntegrationNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_Update_ActiveRowSucceeds confirms the added deleted_at
// guard does not regress the happy path: an active row still matches and updates.
func TestIntegrationRepo_Update_ActiveRowSucceeds(t *testing.T) {
	ctx := context.Background()
	repo, mockPool := newTestIntegrationRepo(t)

	integration := &domain.Integration{
		ID:                   uuid.New(),
		Status:               "active",
		EncryptedAccessToken: []byte("ct"),
		ExternalID:           "tg_1",
		Metadata:             map[string]interface{}{},
	}

	mockPool.ExpectExec(`UPDATE integrations SET.*WHERE.*deleted_at IS NULL`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, integration)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_ReadModifyWrite_PreservesEnvelopeMetadata reproduces the
// metadata/external_id heal flow (GetByID → mutate one field → Update without
// re-encrypting) and asserts the envelope metadata round-trips. Update rewrites
// key_version and encryption_key_fingerprint from the in-memory object, so when
// the read path omits those columns the scanned object carries 0 / "" and the
// write clobbers a correctly-rotated row. The UPDATE expectation pins the
// originally-read key_version (7) and fingerprint ("fp-rotated") as exact args,
// so omitting them from the SELECT/scan fails this test.
func TestIntegrationRepo_ReadModifyWrite_PreservesEnvelopeMetadata(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	businessID := uuid.New()
	now := time.Now()

	repo, mockPool := newTestIntegrationRepo(t)

	readRows := pgxmock.NewRows([]string{
		"id", "business_id", "platform", "status",
		"encrypted_access_token", "encrypted_refresh_token", "encrypted_user_token",
		"external_id", "metadata", "token_expires_at", "user_token_expires_at",
		"created_at", "updated_at",
		"wrapped_dek", "key_version", "encryption_key_fingerprint",
	}).
		AddRow(id, businessID, "yandex_business", "active",
			[]byte("ct"), []byte(nil), []byte(nil),
			"sprav_111", map[string]interface{}{}, (*time.Time)(nil), (*time.Time)(nil),
			now, now, []byte("wrapped-dek"), ptrInt16(7), ptrString("fp-rotated"))

	mockPool.ExpectQuery(`SELECT .+ FROM integrations WHERE deleted_at IS NULL AND id = \$1`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(readRows)

	integration, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int16(7), integration.KeyVersion)
	require.Equal(t, "fp-rotated", integration.EncryptionKeyFingerprint)

	integration.Metadata = map[string]interface{}{"business_name": "healed name"}

	mockPool.ExpectExec(`UPDATE integrations SET.*key_version.*encryption_key_fingerprint.*WHERE.*deleted_at IS NULL`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			[]byte("wrapped-dek"), int16(7), "fp-rotated",
			pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, repo.Update(ctx, integration))
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_UpdateMetadata_TargetedSingleColumn pins the emitted SQL
// to exactly metadata + updated_at (anchored ^...$). The targeted UPDATE must
// never carry a status or encrypted_access_token SET clause: if it did, a
// concurrent MarkTokenExpired flip would be reverted by this write's stale
// snapshot. Routing UpdateMetadata back through the full-row Update SQL (which
// SETs status/encrypted_access_token/... ) no longer matches this anchored
// regexp and fails the test.
func TestIntegrationRepo_UpdateMetadata_TargetedSingleColumn(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`^UPDATE integrations SET metadata = \$1, updated_at = \$2 WHERE deleted_at IS NULL AND id = \$3$`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateMetadata(ctx, id, map[string]interface{}{"business_name": "renamed"})
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_UpdateMetadata_NotFound asserts the deleted_at IS NULL
// guard: a soft-deleted (or absent) row affects zero rows and surfaces
// ErrIntegrationNotFound rather than silently no-op'ing.
func TestIntegrationRepo_UpdateMetadata_NotFound(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET metadata = \$1, updated_at = \$2 WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.UpdateMetadata(ctx, id, map[string]interface{}{})
	require.ErrorIs(t, err, domain.ErrIntegrationNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_UpdateExternalID_TargetedSingleColumn mirrors the
// metadata guard for the external_id heal path: the emitted SQL must SET only
// external_id + updated_at so a concurrent status flip is never reverted.
func TestIntegrationRepo_UpdateExternalID_TargetedSingleColumn(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`^UPDATE integrations SET external_id = \$1, updated_at = \$2 WHERE deleted_at IS NULL AND id = \$3$`).
		WithArgs("sprav_permalink_42", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.UpdateExternalID(ctx, id, "sprav_permalink_42")
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// TestIntegrationRepo_UpdateExternalID_NotFound asserts the deleted_at IS NULL
// guard surfaces ErrIntegrationNotFound for a soft-deleted/absent row.
func TestIntegrationRepo_UpdateExternalID_NotFound(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	repo, mockPool := newTestIntegrationRepo(t)

	mockPool.ExpectExec(`UPDATE integrations SET external_id = \$1, updated_at = \$2 WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.UpdateExternalID(ctx, id, "ext")
	require.ErrorIs(t, err, domain.ErrIntegrationNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}
