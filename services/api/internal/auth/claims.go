package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	TokenIssuer   = "onevoice-api"
	TokenAudience = "onevoice"
)

// AccessTokenClaims represents JWT claims for access tokens.
type AccessTokenClaims struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	Role   string    `json:"role"`
	jwt.RegisteredClaims
}

// RefreshTokenClaims represents JWT claims for refresh tokens.
type RefreshTokenClaims struct {
	UserID  uuid.UUID `json:"user_id"`
	TokenID uuid.UUID `json:"token_id"`
	jwt.RegisteredClaims
}
