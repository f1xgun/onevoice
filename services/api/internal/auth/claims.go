package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	TokenIssuer   = "onevoice-api"
	TokenAudience = "onevoice"

	// TokenSubjectAccess and TokenSubjectRefresh are the token-type
	// discriminators carried in RegisteredClaims.Subject. Access and refresh
	// tokens share the same secret, issuer and audience, so the subject is the
	// only field that distinguishes one from the other. Every parser must pin
	// the subject it expects (jwt.WithSubject) so a refresh token can never be
	// replayed as a bearer access token, and vice versa.
	TokenSubjectAccess  = "access"
	TokenSubjectRefresh = "refresh"

	// JWTSecretMinLen is the minimum acceptable JWT signing-secret length in
	// bytes. HS256 requires at least 256-bit secret strength to resist
	// brute-force, so we enforce 32 bytes everywhere we validate the secret
	// (config load, service constructor).
	JWTSecretMinLen = 32
)

// AccessTokenClaims represents JWT claims for access tokens. The legacy
// "role" claim was removed; jwt/v5's json.Unmarshal ignores unknown fields,
// so older tokens carrying "role":"owner" continue to authenticate for
// one natural-TTL window (15 minutes) until refreshed.
//
// TokenID is decoded purely as a defense-in-depth tripwire: only refresh
// tokens carry a token_id, so the Auth middleware rejects any bearer token
// whose token_id is non-zero even before the subject check, ensuring a
// refresh token can never authenticate as an access token.
type AccessTokenClaims struct {
	UserID  uuid.UUID `json:"user_id"`
	Email   string    `json:"email"`
	TokenID uuid.UUID `json:"token_id"`
	jwt.RegisteredClaims
}

// RefreshTokenClaims represents JWT claims for refresh tokens.
type RefreshTokenClaims struct {
	UserID  uuid.UUID `json:"user_id"`
	TokenID uuid.UUID `json:"token_id"`
	jwt.RegisteredClaims
}
