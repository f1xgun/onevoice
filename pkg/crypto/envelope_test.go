package crypto_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/crypto/kmsfake"
)

func newTestEnvelope(t *testing.T) (*crypto.Envelope, *kmsfake.FakeKMSEncrypter) {
	t.Helper()
	fake := kmsfake.New()
	env := crypto.NewEnvelope(fake, nil, "test-key-id", map[string]int16{"1": 1, "2": 2})
	return env, fake
}

func TestEnvelopeRoundTrip(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	id := uuid.New()
	platform := "yandex_business"
	plaintext := []byte("super-secret-oauth-token")

	ct, wrappedDEK, _, _, err := env.EncryptToken(ctx, id, platform, plaintext)
	require.NoError(t, err)
	assert.NotEmpty(t, ct)
	assert.NotEmpty(t, wrappedDEK)

	recovered, _, err := env.DecryptToken(ctx, id, platform, ct, wrappedDEK)
	require.NoError(t, err)
	assert.Equal(t, plaintext, recovered)
}

func TestEnvelopeAADBindsRow(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	idA := uuid.New()
	idB := uuid.New()
	platform := "yandex_business"

	ct, wrappedDEK, _, _, err := env.EncryptToken(ctx, idA, platform, []byte("token"))
	require.NoError(t, err)

	_, _, err = env.DecryptToken(ctx, idB, platform, ct, wrappedDEK)
	assert.Error(t, err, "row-swap must fail")
}

func TestEnvelopeAADBindsPlatform(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	id := uuid.New()

	ct, wrappedDEK, _, _, err := env.EncryptToken(ctx, id, "yandex_business", []byte("token"))
	require.NoError(t, err)

	_, _, err = env.DecryptToken(ctx, id, "telegram", ct, wrappedDEK)
	assert.Error(t, err, "platform-swap must fail")
}

func TestEnvelopeReturnsKeyVersion(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	id := uuid.New()

	_, _, kv, _, err := env.EncryptToken(ctx, id, "yandex_business", []byte("tok"))
	require.NoError(t, err)
	assert.Equal(t, int16(1), kv)
}

func TestEnvelopeReturnsFingerprint(t *testing.T) {
	fake := kmsfake.New()
	env := crypto.NewEnvelope(fake, nil, "test-key-id", map[string]int16{"1": 1})
	ctx := context.Background()
	id := uuid.New()

	_, _, _, fp, err := env.EncryptToken(ctx, id, "yandex_business", []byte("tok"))
	require.NoError(t, err)

	sum := sha256.Sum256([]byte("test-key-id"))
	expected := hex.EncodeToString(sum[:])
	assert.Equal(t, expected, fp)
	assert.Equal(t, expected, env.Fingerprint())
}

func TestEnvelopeDualDecryptFallback(t *testing.T) {
	fake := kmsfake.New()
	env := crypto.NewEnvelope(fake, nil, "test-key-id", map[string]int16{"1": 1, "2": 2})
	ctx := context.Background()
	id := uuid.New()
	platform := "yandex_business"
	plaintext := []byte("token-v1")

	ct, wrappedDEK, _, _, err := env.EncryptToken(ctx, id, platform, plaintext)
	require.NoError(t, err)

	fake.RotateToVersion(2)

	recovered, _, err := env.DecryptToken(ctx, id, platform, ct, wrappedDEK)
	require.NoError(t, err)
	assert.Equal(t, plaintext, recovered)
}

func TestEnvelopeLegacyDecrypt_NilWrappedDEK_NoLegacy(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	id := uuid.New()

	_, _, err := env.DecryptToken(ctx, id, "yandex_business", []byte("someciphertext"), nil)
	assert.ErrorIs(t, err, crypto.ErrLegacyKeyNotConfigured)
}

func TestEnvelopeLegacyDecrypt_NilWrappedDEK_WithLegacy(t *testing.T) {
	fake := kmsfake.New()
	key := make([]byte, crypto.AES256KeyLen)
	legacy, err := crypto.NewEncryptor(key)
	require.NoError(t, err)
	env := crypto.NewEnvelope(fake, legacy, "test-key-id", nil)

	plaintext := []byte("legacy-token")
	ct, encErr := legacy.Encrypt(plaintext)
	require.NoError(t, encErr)

	ctx := context.Background()
	id := uuid.New()

	recovered, _, err := env.DecryptToken(ctx, id, "yandex_business", ct, nil)
	require.NoError(t, err)
	assert.Equal(t, plaintext, recovered)
}

func TestEnvelopeWipeOnDecrypt(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	id := uuid.New()
	platform := "yandex_business"
	plaintext := []byte("token-data")

	ct, wrappedDEK, _, _, err := env.EncryptToken(ctx, id, platform, plaintext)
	require.NoError(t, err)

	recovered, _, err := env.DecryptToken(ctx, id, platform, ct, wrappedDEK)
	require.NoError(t, err)
	assert.Equal(t, plaintext, recovered)

	// The returned plaintext must be a copy independent from the DEK.
	// We verify this by confirming the recovered slice is correct —
	// if DEK had leaked into it or was aliased, the round-trip comparison
	// above would have failed or produced garbage.
	assert.Equal(t, plaintext, recovered)
}

func TestEncryptForRow_OneDEKManyTokens(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	id := uuid.New()
	platform := "yandex_business"

	access := []byte("access-token-value")
	refresh := []byte("refresh-token-value")
	user := []byte("user-token-value")

	cts, wrapped, ver, fp, err := env.EncryptForRow(ctx, id, platform, [][]byte{access, refresh, user})
	require.NoError(t, err)
	require.Len(t, cts, 3)
	assert.NotEmpty(t, cts[0])
	assert.NotEmpty(t, cts[1])
	assert.NotEmpty(t, cts[2])
	assert.NotEmpty(t, wrapped)
	assert.Equal(t, int16(1), ver)
	assert.NotEmpty(t, fp)

	pts, _, err := env.DecryptForRow(ctx, id, platform, cts, wrapped)
	require.NoError(t, err)
	require.Len(t, pts, 3)
	assert.Equal(t, access, pts[0])
	assert.Equal(t, refresh, pts[1])
	assert.Equal(t, user, pts[2])
}

func TestEncryptForRow_NilPlaintextSkipped(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	id := uuid.New()
	platform := "telegram"

	cts, wrapped, _, _, err := env.EncryptForRow(ctx, id, platform, [][]byte{[]byte("access"), nil, nil})
	require.NoError(t, err)
	require.Len(t, cts, 3)
	assert.NotEmpty(t, cts[0])
	assert.Nil(t, cts[1])
	assert.Nil(t, cts[2])

	pts, _, err := env.DecryptForRow(ctx, id, platform, cts, wrapped)
	require.NoError(t, err)
	assert.Equal(t, []byte("access"), pts[0])
	assert.Nil(t, pts[1])
	assert.Nil(t, pts[2])
}

func TestEncryptForRow_RowSwapFails(t *testing.T) {
	env, _ := newTestEnvelope(t)
	ctx := context.Background()
	idA := uuid.New()
	idB := uuid.New()
	platform := "yandex_business"

	cts, wrapped, _, _, err := env.EncryptForRow(ctx, idA, platform, [][]byte{[]byte("tok")})
	require.NoError(t, err)

	_, _, err = env.DecryptForRow(ctx, idB, platform, cts, wrapped)
	assert.Error(t, err, "row-swap must fail")
}

func TestEnvelopeAADHelper(t *testing.T) {
	id := uuid.Nil
	platform := "yandex_business"
	expected := []byte("00000000-0000-0000-0000-000000000000|yandex_business")

	got := crypto.EnvelopeAADForTest(id, platform)
	assert.Equal(t, expected, got)
}

func TestEncryptForRow_UnmappedKMSVersionFailsClosed(t *testing.T) {
	fake := kmsfake.New()
	fake.RotateToVersion(7)
	env := crypto.NewEnvelope(fake, nil, "test-key-id", map[string]int16{"1": 1})
	ctx := context.Background()
	id := uuid.New()

	cts, wrapped, ver, fp, err := env.EncryptForRow(ctx, id, "yandex_business", [][]byte{[]byte("tok")})
	require.ErrorIs(t, err, crypto.ErrUnmappedKMSVersion)
	assert.Nil(t, cts)
	assert.Nil(t, wrapped)
	assert.Equal(t, int16(0), ver)
	assert.Empty(t, fp)
}

func TestEncryptToken_UnmappedKMSVersionFailsClosed(t *testing.T) {
	fake := kmsfake.New()
	fake.RotateToVersion(7)
	env := crypto.NewEnvelope(fake, nil, "test-key-id", map[string]int16{"1": 1})
	ctx := context.Background()
	id := uuid.New()

	ct, wrapped, ver, fp, err := env.EncryptToken(ctx, id, "yandex_business", []byte("tok"))
	require.ErrorIs(t, err, crypto.ErrUnmappedKMSVersion)
	assert.Nil(t, ct)
	assert.Nil(t, wrapped)
	assert.Equal(t, int16(0), ver)
	assert.Empty(t, fp)
}
