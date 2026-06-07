package kmsfake

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
)

const (
	maxAADLen          = math.MaxUint16
	ciphertextHeaderSz = 4
)

// FakeKMSEncrypter is an in-memory implementation of the KMSEncrypter interface
// for use in unit tests. It encodes ciphertext as:
//
//	uint16(len(version)) || version || uint16(len(aad)) || aad || plaintext
//
// so that Decrypt can reverse it exactly. AAD mismatch returns an error
// mirroring AES-GCM semantics.
type FakeKMSEncrypter struct {
	mu             sync.Mutex
	currentVersion int
	knownVersions  map[string]struct{}
}

// New returns a FakeKMSEncrypter at version "1".
func New() *FakeKMSEncrypter {
	return &FakeKMSEncrypter{
		currentVersion: 1,
		knownVersions:  map[string]struct{}{"1": {}},
	}
}

// RotateToVersion advances the current version to v and registers it as known.
func (f *FakeKMSEncrypter) RotateToVersion(v int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentVersion = v
	f.knownVersions[strconv.Itoa(v)] = struct{}{}
}

// Encrypt encodes plaintext with the current version and the provided aad.
// Returns the encoded ciphertext, the current version string, and nil error.
func (f *FakeKMSEncrypter) Encrypt(_ context.Context, plaintext, aad []byte) (ciphertext []byte, versionID string, err error) {
	f.mu.Lock()
	version := strconv.Itoa(f.currentVersion)
	f.mu.Unlock()

	if len(version) > maxAADLen {
		return nil, "", errors.New("kmsfake: version label too large")
	}
	if len(aad) > maxAADLen {
		return nil, "", errors.New("kmsfake: aad too large")
	}
	out := make([]byte, 0, 2+len(version)+2+len(aad)+len(plaintext))
	out = binary.BigEndian.AppendUint16(out, uint16(len(version))) //nolint:gosec // bounded by maxAADLen above
	out = append(out, []byte(version)...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(aad))) //nolint:gosec // bounded by maxAADLen above
	out = append(out, aad...)
	out = append(out, plaintext...)
	return out, version, nil
}

// Decrypt reverses the encoding produced by Encrypt. Returns an error if
// ciphertext is malformed, the version is unknown, or the aad does not match.
func (f *FakeKMSEncrypter) Decrypt(_ context.Context, ciphertext, aad []byte) (plaintext []byte, versionID string, err error) {
	if len(ciphertext) < ciphertextHeaderSz {
		return nil, "", errors.New("kmsfake: ciphertext too short")
	}
	vLen := binary.BigEndian.Uint16(ciphertext[:2])
	if int(vLen)+4 > len(ciphertext) {
		return nil, "", errors.New("kmsfake: truncated version header")
	}
	version := string(ciphertext[2 : 2+vLen])
	rest := ciphertext[2+vLen:]
	aLen := binary.BigEndian.Uint16(rest[:2])
	if int(aLen)+2 > len(rest) {
		return nil, version, errors.New("kmsfake: truncated aad header")
	}
	gotAAD := rest[2 : 2+aLen]
	body := rest[2+aLen:]

	f.mu.Lock()
	_, known := f.knownVersions[version]
	f.mu.Unlock()
	if !known {
		return nil, version, fmt.Errorf("kmsfake: unknown version %q", version)
	}
	if !bytes.Equal(gotAAD, aad) {
		return nil, version, errors.New("kmsfake: aad mismatch")
	}
	out := make([]byte, len(body))
	copy(out, body)
	return out, version, nil
}
