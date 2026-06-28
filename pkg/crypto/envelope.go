package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// ErrLegacyKeyNotConfigured is returned when DecryptToken is called with a nil
// wrappedDEK (legacy row) but no legacy Encryptor was provided at construction.
var ErrLegacyKeyNotConfigured = errors.New("crypto: legacy key required to decrypt this row but not configured")

// unknownVersionWarned suppresses repeated log warnings for the same unknown
// KMS version ID string. Stored values are struct{}.
var unknownVersionWarned sync.Map

// ErrUnknownKeyVersion is returned when the key version from KMS is not in the
// dual-decrypt version map.
var ErrUnknownKeyVersion = errors.New("crypto: ciphertext key version not in dual-decrypt set")

// ErrUnmappedKMSVersion is returned by the encrypt paths when the KMS Encrypt
// response carries a non-empty version ID that is absent from the configured
// version map. Persisting key_version=0 in that case would silently corrupt the
// rekey audit metadata and wedge rotation, so the encrypt call fails closed and
// the operator must add the version to TOKEN_ENCRYPTION_KMS_VERSION_MAP.
var ErrUnmappedKMSVersion = errors.New("crypto: KMS version ID not in version map")

// Envelope performs per-record DEK envelope encryption. Each call to
// EncryptToken generates a fresh AES-256 DEK, wraps it via KMS, and seals
// the plaintext with AES-GCM using the same AAD at both layers.
type Envelope struct {
	kms         KMSEncrypter
	legacy      *Encryptor
	fingerprint string
	versionMap  map[string]int16
}

// NewEnvelope constructs an Envelope. kmsKeyID is SHA-256 hashed to produce
// the fingerprint stored on each row. versionMap maps KMS version ID strings
// to the int16 values persisted in the DB; nil is treated as empty.
// legacy may be nil when no dual-read window is needed.
func NewEnvelope(kms KMSEncrypter, legacy *Encryptor, kmsKeyID string, versionMap map[string]int16) *Envelope {
	sum := sha256.Sum256([]byte(kmsKeyID))
	if versionMap == nil {
		versionMap = map[string]int16{}
	}
	return &Envelope{
		kms:         kms,
		legacy:      legacy,
		fingerprint: hex.EncodeToString(sum[:]),
		versionMap:  versionMap,
	}
}

// Fingerprint returns the SHA-256 hex of the KMS key ID supplied at construction.
func (e *Envelope) Fingerprint() string { return e.fingerprint }

// envelopeAAD returns the AAD used for both the KMS wrap and the AES-GCM seal.
// Format: "<integrationID>|<platform>".
func envelopeAAD(integrationID uuid.UUID, platform string) []byte {
	return []byte(integrationID.String() + "|" + platform)
}

// EncryptToken generates a fresh AES-256 DEK, wraps it via KMS with AAD bound
// to (integrationID, platform), seals the plaintext with AES-GCM using the
// same AAD, and returns all three artifacts. The DEK is Wiped before return.
func (e *Envelope) EncryptToken(
	ctx context.Context,
	integrationID uuid.UUID,
	platform string,
	plaintext []byte,
) (ciphertext, wrappedDEK []byte, keyVersion int16, fingerprint string, err error) {
	dek := make([]byte, AES256KeyLen)
	if _, rerr := io.ReadFull(rand.Reader, dek); rerr != nil {
		return nil, nil, 0, "", fmt.Errorf("crypto: dek gen: %w", rerr)
	}
	defer Wipe(dek)

	aad := envelopeAAD(integrationID, platform)
	wrapped, versionStr, kerr := e.kms.Encrypt(ctx, dek, aad)
	if kerr != nil {
		return nil, nil, 0, "", fmt.Errorf("crypto: kms wrap: %w", kerr)
	}

	ct, serr := aeadSeal(dek, plaintext, aad)
	if serr != nil {
		return nil, nil, 0, "", fmt.Errorf("crypto: aead seal: %w", serr)
	}
	ver, verr := e.resolveVersionForEncrypt(versionStr)
	if verr != nil {
		return nil, nil, 0, "", verr
	}
	return ct, wrapped, ver, e.fingerprint, nil
}

// DecryptToken unwraps the DEK via KMS, verifies AAD at both layers, and
// returns the plaintext. The DEK is Wiped before return. When wrappedDEK is
// nil the call falls back to the legacy Encryptor (dual-read window); if no
// legacy Encryptor is configured, ErrLegacyKeyNotConfigured is returned.
func (e *Envelope) DecryptToken(
	ctx context.Context,
	integrationID uuid.UUID,
	platform string,
	ciphertext, wrappedDEK []byte,
) (plaintext []byte, keyVersion int16, err error) {
	if wrappedDEK == nil {
		if e.legacy == nil {
			return nil, 0, ErrLegacyKeyNotConfigured
		}
		pt, derr := e.legacy.Decrypt(ciphertext)
		if derr != nil {
			return nil, 0, fmt.Errorf("crypto: legacy decrypt: %w", derr)
		}
		return pt, 0, nil
	}

	aad := envelopeAAD(integrationID, platform)
	dek, versionStr, kerr := e.kms.Decrypt(ctx, wrappedDEK, aad)
	if kerr != nil {
		return nil, 0, fmt.Errorf("crypto: kms unwrap: %w", kerr)
	}
	defer Wipe(dek)

	pt, oerr := aeadOpen(dek, ciphertext, aad)
	if oerr != nil {
		return nil, 0, fmt.Errorf("crypto: aead open: %w", oerr)
	}
	return pt, e.resolveVersion(versionStr), nil
}

// EncryptForRow generates a single AES-256 DEK, wraps it via KMS once, and
// seals each plaintext in plaintexts with AES-GCM using the same AAD. All
// ciphertexts share one wrapped DEK — callers store wrapped + version +
// fingerprint once per row. The DEK is Wiped before return.
// When no KMS client is configured (legacy-only mode), falls back to per-field
// legacy encryption; the returned wrappedDEK is nil and fingerprint is empty.
func (e *Envelope) EncryptForRow(
	ctx context.Context,
	integrationID uuid.UUID,
	platform string,
	plaintexts [][]byte,
) (ciphertexts [][]byte, wrappedDEK []byte, keyVersion int16, fingerprint string, err error) {
	if e.kms == nil {
		if e.legacy == nil {
			return nil, nil, 0, "", ErrLegacyKeyNotConfigured
		}
		cts := make([][]byte, len(plaintexts))
		for i, pt := range plaintexts {
			if len(pt) == 0 {
				cts[i] = nil
				continue
			}
			ct, lerr := e.legacy.Encrypt(pt)
			if lerr != nil {
				return nil, nil, 0, "", fmt.Errorf("crypto: legacy encrypt[%d]: %w", i, lerr)
			}
			cts[i] = ct
		}
		return cts, nil, 0, "", nil
	}

	dek := make([]byte, AES256KeyLen)
	if _, rerr := io.ReadFull(rand.Reader, dek); rerr != nil {
		return nil, nil, 0, "", fmt.Errorf("crypto: dek gen: %w", rerr)
	}
	defer Wipe(dek)

	aad := envelopeAAD(integrationID, platform)
	wrapped, versionStr, kerr := e.kms.Encrypt(ctx, dek, aad)
	if kerr != nil {
		return nil, nil, 0, "", fmt.Errorf("crypto: kms wrap: %w", kerr)
	}

	cts := make([][]byte, len(plaintexts))
	for i, pt := range plaintexts {
		if len(pt) == 0 {
			cts[i] = nil
			continue
		}
		ct, serr := aeadSeal(dek, pt, aad)
		if serr != nil {
			return nil, nil, 0, "", fmt.Errorf("crypto: aead seal[%d]: %w", i, serr)
		}
		cts[i] = ct
	}
	ver, verr := e.resolveVersionForEncrypt(versionStr)
	if verr != nil {
		return nil, nil, 0, "", verr
	}
	return cts, wrapped, ver, e.fingerprint, nil
}

// DecryptForRow unwraps the shared DEK via KMS and decrypts each ciphertext
// in ciphertexts. nil entries in ciphertexts produce nil plaintexts. The DEK
// is Wiped before return.
func (e *Envelope) DecryptForRow(
	ctx context.Context,
	integrationID uuid.UUID,
	platform string,
	ciphertexts [][]byte,
	wrappedDEK []byte,
) (plaintexts [][]byte, keyVersion int16, err error) {
	aad := envelopeAAD(integrationID, platform)
	dek, versionStr, kerr := e.kms.Decrypt(ctx, wrappedDEK, aad)
	if kerr != nil {
		return nil, 0, fmt.Errorf("crypto: kms unwrap: %w", kerr)
	}
	defer Wipe(dek)

	pts := make([][]byte, len(ciphertexts))
	for i, ct := range ciphertexts {
		if len(ct) == 0 {
			pts[i] = nil
			continue
		}
		pt, oerr := aeadOpen(dek, ct, aad)
		if oerr != nil {
			return nil, 0, fmt.Errorf("crypto: aead open[%d]: %w", i, oerr)
		}
		pts[i] = pt
	}
	return pts, e.resolveVersion(versionStr), nil
}

// resolveVersion maps a KMS version ID string to its persisted int16. It is the
// lenient form used on the decrypt paths, where a version absent from the map
// (legacy/dual-decrypt rows) must still resolve to 0 rather than fail the read.
func (e *Envelope) resolveVersion(versionStr string) int16 {
	if v, ok := e.versionMap[versionStr]; ok {
		return v
	}
	if versionStr != "" {
		if _, alreadyWarned := unknownVersionWarned.LoadOrStore(versionStr, struct{}{}); !alreadyWarned {
			slog.Warn("crypto: KMS version ID not in version map; key_version recorded as 0",
				"version_id", versionStr)
		}
	}
	return 0
}

// resolveVersionForEncrypt maps a KMS version ID string to its persisted int16
// for the encrypt paths and fails closed: when the KMS Encrypt response carries
// a non-empty version ID that is not in the map, it returns ErrUnmappedKMSVersion
// rather than silently persisting key_version=0. An empty version ID (legacy /
// non-KMS adapters that do not surface a version) maps to 0 with no error.
func (e *Envelope) resolveVersionForEncrypt(versionStr string) (int16, error) {
	if v, ok := e.versionMap[versionStr]; ok {
		return v, nil
	}
	if versionStr != "" {
		return 0, fmt.Errorf("%w: %q", ErrUnmappedKMSVersion, versionStr)
	}
	return 0, nil
}

func aeadSeal(key, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm new: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

func aeadOpen(key, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm new: %w", err)
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, ErrInvalidCiphertext
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	pt, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return pt, nil
}
