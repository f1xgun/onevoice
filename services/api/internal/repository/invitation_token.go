// Package repository — invitation_token.go
//
// GenerateInvitationToken produces the raw URL-safe invitation token
// (returned to the inviter ONCE at create time, per CONTEXT D-08 / INVITE-01)
// and the hex-encoded sha256 hash that gets persisted in
// invitations.token_hash. The 32 random bytes give 256 bits of entropy
// — well above the 128-bit floor for unguessable opaque tokens.
//
// base64.RawURLEncoding (no padding) is chosen so the token is safe in URLs
// without %-encoding gymnastics. Length is constant: 32 bytes → 43
// base64-RawURL chars, 0 padding. sha256 → 32 bytes → 64 hex chars.
//
// Lookup correctness (INVITE-02 spec phrase "crypto/subtle.ConstantTimeCompare")
// is satisfied STRUCTURALLY by hash equality on the UNIQUE token_hash B-tree
// index — Postgres index lookup is content-time-bounded by depth, not by the
// byte-by-byte hash content. DO NOT add an in-process subtle.ConstantTimeCompare
// loop on top of the DB lookup; doing so would actually weaken the property.
// See 03-RESEARCH.md §"Token Hashing & Lookup" lines 64-80.
package repository

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// GenerateInvitationToken returns (raw, hash, err).
//   - raw: 43-char base64-RawURL string suitable for embedding in an /invite/<token> URL.
//   - hash: 64-char lowercase hex sha256 of raw — persisted in invitations.token_hash.
//   - err: only on crypto/rand.Read failure (essentially never; OS entropy starvation).
func GenerateInvitationToken() (raw, hash string, err error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", fmt.Errorf("crypto/rand: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf[:])
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, hash, nil
}
