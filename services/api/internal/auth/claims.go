package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	TokenIssuer   = "onevoice-api"
	TokenAudience = "onevoice"

	// JWTSecretMinLen is the minimum acceptable JWT signing-secret length in
	// bytes. HS256 requires at least 256-bit secret strength to resist
	// brute-force, so we enforce 32 bytes everywhere we validate the secret
	// (config load, service constructor).
	JWTSecretMinLen = 32
)

// AccessTokenClaims represents JWT claims for access tokens.
//
// v2.0 RBAC milestone (Phase 6, CLEAN-02): the legacy "role" claim was
// removed. jwt/v5's json.Unmarshal ignores unknown fields, so access tokens
// issued before Phase 6 (carrying "role":"owner") continue to authenticate
// for one natural-TTL window (15 minutes) until refreshed.
type AccessTokenClaims struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	jwt.RegisteredClaims
}

// RefreshTokenClaims represents JWT claims for refresh tokens.
type RefreshTokenClaims struct {
	UserID  uuid.UUID `json:"user_id"`
	TokenID uuid.UUID `json:"token_id"`
	jwt.RegisteredClaims
}
