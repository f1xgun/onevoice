package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateInvitationToken_Length(t *testing.T) {
	raw, hash, err := GenerateInvitationToken()
	require.NoError(t, err)
	require.Len(t, raw, 43, "32-byte source under base64.RawURLEncoding (no padding) yields exactly 43 chars")
	require.Len(t, hash, 64, "sha256 hex-encoded yields exactly 64 chars")
}

func TestGenerateInvitationToken_Charset(t *testing.T) {
	raw, hash, err := GenerateInvitationToken()
	require.NoError(t, err)

	rawAlphabet := regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	hashAlphabet := regexp.MustCompile(`^[0-9a-f]+$`)

	require.Regexp(t, rawAlphabet, raw, "raw token must use base64.RawURLEncoding alphabet")
	require.Regexp(t, hashAlphabet, hash, "hash must be lowercase hex")
}

func TestGenerateInvitationToken_Distinct(t *testing.T) {
	const N = 100
	rawSeen := make(map[string]struct{}, N)
	hashSeen := make(map[string]struct{}, N)
	for i := 0; i < N; i++ {
		raw, hash, err := GenerateInvitationToken()
		require.NoError(t, err)
		_, dupRaw := rawSeen[raw]
		_, dupHash := hashSeen[hash]
		require.False(t, dupRaw, "duplicate raw token at i=%d", i)
		require.False(t, dupHash, "duplicate hash at i=%d", i)
		rawSeen[raw] = struct{}{}
		hashSeen[hash] = struct{}{}
	}
	require.Len(t, rawSeen, N)
	require.Len(t, hashSeen, N)
}

func TestGenerateInvitationToken_HashConsistency(t *testing.T) {
	raw, hash, err := GenerateInvitationToken()
	require.NoError(t, err)

	// External recompute of sha256(raw) — must equal the helper's hash.
	// This proves the lookup path (which computes sha256(raw) from the URL
	// param) will find the same row the create path inserted.
	sum := sha256.Sum256([]byte(raw))
	expected := hex.EncodeToString(sum[:])
	require.Equal(t, expected, hash, "helper's hash must equal externally recomputed sha256(raw)")
}
