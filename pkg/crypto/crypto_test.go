package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptor_EncryptDecrypt(t *testing.T) {
	key := []byte("01234567890123456789012345678901") // 32 bytes
	encryptor, err := NewEncryptor(key)
	require.NoError(t, err)

	plaintext := []byte("secret_oauth_token_12345")

	// Encrypt
	ciphertext, err := encryptor.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	// Decrypt
	decrypted, err := encryptor.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptor_InvalidKey(t *testing.T) {
	shortKey := []byte("tooshort")
	_, err := NewEncryptor(shortKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestEncryptor_InvalidCiphertext(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	encryptor, err := NewEncryptor(key)
	require.NoError(t, err)

	_, err = encryptor.Decrypt([]byte("invalid"))
	assert.ErrorIs(t, err, ErrInvalidCiphertext)
}
