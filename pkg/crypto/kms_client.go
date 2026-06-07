package crypto

import "context"

// KMSEncrypter wraps a KMS service. Concrete implementations live in service
// wire packages so pkg/crypto stays SDK-free. AAD MUST be passed identically
// on Encrypt and Decrypt; mismatch on Decrypt yields a non-nil error.
type KMSEncrypter interface {
	Encrypt(ctx context.Context, plaintext, aad []byte) (ciphertext []byte, versionID string, err error)
	Decrypt(ctx context.Context, ciphertext, aad []byte) (plaintext []byte, versionID string, err error)
}
