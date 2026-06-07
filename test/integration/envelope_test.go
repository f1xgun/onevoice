package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/crypto/kmsfake"
)

// envelopeTestPool opens a pgxpool to TEST_POSTGRES_URL and ensures the
// integrations table has the phase-30 envelope columns (wrapped_dek,
// key_version, encryption_key_fingerprint). If those columns are absent the
// ALTER TABLE is run inline so the test can proceed against a database that
// only has phase-28 migrations applied.
func envelopeTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping envelope integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test postgres")
	t.Cleanup(pool.Close)

	ensureEnvelopeColumns(t, pool)
	return pool
}

// ensureEnvelopeColumns adds wrapped_dek / key_version / encryption_key_fingerprint
// to the integrations table when they are missing. This makes the tests
// runnable against a database that is on an older migration version.
func ensureEnvelopeColumns(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`ALTER TABLE integrations ADD COLUMN IF NOT EXISTS wrapped_dek BYTEA NULL`,
		`ALTER TABLE integrations ADD COLUMN IF NOT EXISTS key_version SMALLINT NULL`,
		`ALTER TABLE integrations ADD COLUMN IF NOT EXISTS encryption_key_fingerprint TEXT NULL`,
		`ALTER TABLE integrations ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL`,
	}
	for _, stmt := range stmts {
		_, err := pool.Exec(ctx, stmt)
		require.NoError(t, err, "ensure envelope column: %s", stmt)
	}
}

// seedBusiness inserts a minimal business row and returns its id.
func seedBusiness(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO businesses (id, name, category, address, phone, description, logo_url, settings)
		VALUES ($1, 'Test Business', 'test', '1 Main St', '+10000000000', '', '', '{}')
	`, id)
	require.NoError(t, err, "seed business")
	return id
}

// seedEnvelopeRow inserts an integration row with envelope-encrypted tokens.
// Returns the integration id and the ciphertext + wrappedDEK for the access token.
func seedEnvelopeRow(
	t *testing.T,
	pool *pgxpool.Pool,
	env *crypto.Envelope,
	businessID uuid.UUID,
	platform string,
	plaintext string,
) (id uuid.UUID, encAccess, wrappedDEK []byte) {
	t.Helper()
	ctx := context.Background()
	id = uuid.New()
	pts := [][]byte{[]byte(plaintext)}
	cts, wd, kv, fp, err := env.EncryptForRow(ctx, id, platform, pts)
	require.NoError(t, err, "encrypt for row")

	_, err = pool.Exec(ctx, `
		INSERT INTO integrations
		  (id, business_id, platform, status, encrypted_access_token, wrapped_dek, key_version,
		   encryption_key_fingerprint, external_id, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, 'active', $4, $5, $6, $7, $8, '{}', NOW(), NOW())
	`, id, businessID, platform, cts[0], wd, kv, fp, id.String())
	require.NoError(t, err, "insert envelope row")

	return id, cts[0], wd
}

// TestEnvelopeRowSwapRejected verifies that cross-row decryption fails.
// An envelope row's wrappedDEK is bound to its own (integrationID, platform)
// via AAD. Swapping the ciphertext+wrappedDEK into a different row's columns
// must cause the KMS layer to return an error.
func TestEnvelopeRowSwapRejected(t *testing.T) {
	pool := envelopeTestPool(t)
	ctx := context.Background()

	kms := kmsfake.New()
	env := crypto.NewEnvelope(kms, nil, "test-kms-key-id", map[string]int16{"1": 1})

	bizID := seedBusiness(t, pool)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})

	idA, encAccessA, wrappedDEKA := seedEnvelopeRow(t, pool, env, bizID, "yandex_business", "tokenA-plaintext")
	idB := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO integrations
		  (id, business_id, platform, status, encrypted_access_token, wrapped_dek, key_version,
		   encryption_key_fingerprint, external_id, metadata, created_at, updated_at)
		VALUES ($1, $2, 'yandex_business', 'active', $3, $4, 1, $5, $6, '{}', NOW(), NOW())
	`, idB, bizID, encAccessA, wrappedDEKA, env.Fingerprint(), idB.String())
	require.NoError(t, err, "insert cross-row integration")

	// idB's row now holds idA's ciphertext+wrappedDEK.
	// Decrypting with idB as the integrationID must fail (AAD = "idB|yandex_business" != "idA|yandex_business").
	_, _, decErr := env.DecryptToken(ctx, idB, "yandex_business", encAccessA, wrappedDEKA)
	require.Error(t, decErr, "expected AAD mismatch error for row-swap scenario")
	require.Contains(t, decErr.Error(), "kms", "error should reference kms layer")
	_ = idA
}

// TestEnvelopeLegacyDualRead verifies that GetDecryptedToken-equivalent logic
// returns the correct plaintext for both a legacy flat-AES row (wrapped_dek IS
// NULL) and a modern envelope row, using the same Envelope instance.
func TestEnvelopeLegacyDualRead(t *testing.T) {
	pool := envelopeTestPool(t)
	ctx := context.Background()

	legacyKey := []byte("32-byte-legacy-key-padded-000000")
	legacyEnc, err := crypto.NewEncryptor(legacyKey)
	require.NoError(t, err)

	kms := kmsfake.New()
	env := crypto.NewEnvelope(kms, legacyEnc, "test-kms-key-id", map[string]int16{"1": 1})

	bizID := seedBusiness(t, pool)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})

	legacyPlain := "legacy-plaintext-access-token"
	legacyCT, err := legacyEnc.Encrypt([]byte(legacyPlain))
	require.NoError(t, err)

	legacyID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO integrations
		  (id, business_id, platform, status, encrypted_access_token, wrapped_dek,
		   encryption_key_fingerprint, external_id, metadata, created_at, updated_at)
		VALUES ($1, $2, 'telegram', 'active', $3, NULL, NULL, $4, '{}', NOW(), NOW())
	`, legacyID, bizID, legacyCT, legacyID.String())
	require.NoError(t, err, "insert legacy row")

	envelopePlain := "envelope-plaintext-access-token"
	envelopeID, _, _ := seedEnvelopeRow(t, pool, env, bizID, "telegram", envelopePlain)

	// Decrypt legacy row: wrappedDEK IS NULL → falls back to legacy Encryptor.
	gotLegacy, _, err := env.DecryptToken(ctx, legacyID, "telegram", legacyCT, nil)
	require.NoError(t, err, "legacy decrypt must succeed")
	require.Equal(t, legacyPlain, string(gotLegacy), "legacy plaintext must match")

	// Read envelope row's ciphertext from DB.
	var encAccess, wrappedDEK []byte
	err = pool.QueryRow(ctx, `SELECT encrypted_access_token, wrapped_dek FROM integrations WHERE id = $1`, envelopeID).
		Scan(&encAccess, &wrappedDEK)
	require.NoError(t, err)

	gotEnvelope, _, err := env.DecryptToken(ctx, envelopeID, "telegram", encAccess, wrappedDEK)
	require.NoError(t, err, "envelope decrypt must succeed")
	require.Equal(t, envelopePlain, string(gotEnvelope), "envelope plaintext must match")
}

// TestEnvelopeKeyVersionPersisted verifies that after an encrypted insert the
// row's key_version column equals the int16 from the version map and the
// fingerprint is non-empty.
func TestEnvelopeKeyVersionPersisted(t *testing.T) {
	pool := envelopeTestPool(t)
	ctx := context.Background()

	kmsID := fmt.Sprintf("test-key-%d", time.Now().UnixNano())
	kms := kmsfake.New()
	env := crypto.NewEnvelope(kms, nil, kmsID, map[string]int16{"1": 1})

	bizID := seedBusiness(t, pool)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})

	integID, _, _ := seedEnvelopeRow(t, pool, env, bizID, "vk", "some-access-token")

	var keyVersion *int16
	var fp *string
	var wdLen int
	err := pool.QueryRow(ctx, `
		SELECT key_version, encryption_key_fingerprint, octet_length(wrapped_dek)
		FROM integrations WHERE id = $1
	`, integID).Scan(&keyVersion, &fp, &wdLen)
	require.NoError(t, err)

	require.NotNil(t, keyVersion, "key_version must not be NULL for a newly created envelope row")
	require.Equal(t, int16(1), *keyVersion, "key_version must equal the version map value for version '1'")
	require.NotNil(t, fp, "encryption_key_fingerprint must not be NULL")
	require.NotEmpty(t, *fp, "encryption_key_fingerprint must be non-empty")
	require.Equal(t, env.Fingerprint(), *fp, "fingerprint must match the envelope's computed value")
	require.Greater(t, wdLen, 0, "wrapped_dek must have non-zero length")
}
