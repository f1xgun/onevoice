package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// opaqueTokenEntropyBytes is the raw entropy behind email-verification and
// password-reset tokens (256 bits).
const opaqueTokenEntropyBytes = 32

// generateOpaqueToken returns a url-safe base64 plaintext token and its
// SHA-256 hash. The plaintext travels in the email/browser; only the hash is
// persisted, so the DB never holds a usable secret.
func generateOpaqueToken() (plaintext string, hash []byte, err error) {
	b := make([]byte, opaqueTokenEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	plaintext = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, sum[:], nil
}
