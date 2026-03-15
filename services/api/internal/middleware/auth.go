package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/services/api/internal/auth"
)

// Context keys for storing user information
type contextKey string

const (
	UserIDKey    contextKey = "userID"
	UserEmailKey contextKey = "email"
	UserRoleKey  contextKey = "role"
)

// ErrorResponse represents a JSON error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// Auth creates a JWT authentication middleware
func Auth(jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			tokenString, err := extractToken(r)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, err.Error())
				return
			}

			// Parse and validate token
			token, err := jwt.ParseWithClaims(tokenString, &auth.AccessTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return jwtSecret, nil
			}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(auth.TokenIssuer), jwt.WithAudience(auth.TokenAudience))

			if err != nil {
				switch {
				case errors.Is(err, jwt.ErrTokenExpired):
					writeJSONError(w, http.StatusUnauthorized, "token_expired")
				case errors.Is(err, jwt.ErrSignatureInvalid):
					writeJSONError(w, http.StatusUnauthorized, "token_invalid")
				default:
					writeJSONError(w, http.StatusUnauthorized, "token_invalid")
				}
				return
			}

			claims, ok := token.Claims.(*auth.AccessTokenClaims)
			if !ok || !token.Valid {
				writeJSONError(w, http.StatusUnauthorized, "token_invalid")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, UserEmailKey, claims.Email)
			ctx = context.WithValue(ctx, UserRoleKey, claims.Role)

			// Call next handler with updated context
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractToken extracts the JWT token from the Authorization header
func extractToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header")
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", fmt.Errorf("invalid authorization header format")
	}

	if parts[1] == "" {
		return "", fmt.Errorf("invalid authorization header format")
	}

	return parts[1], nil
}

// GetUserID retrieves the user ID from the request context
func GetUserID(ctx context.Context) (uuid.UUID, error) {
	userID, ok := ctx.Value(UserIDKey).(uuid.UUID)
	if !ok {
		return uuid.Nil, fmt.Errorf("user_id not found in context or invalid type")
	}
	return userID, nil
}

// GetUserEmail retrieves the user email from the request context
func GetUserEmail(ctx context.Context) (string, error) {
	email, ok := ctx.Value(UserEmailKey).(string)
	if !ok {
		return "", fmt.Errorf("email not found in context")
	}
	return email, nil
}

// GetUserRole retrieves the user role from the request context
func GetUserRole(ctx context.Context) (string, error) {
	role, ok := ctx.Value(UserRoleKey).(string)
	if !ok {
		return "", fmt.Errorf("role not found in context")
	}
	return role, nil
}

// writeJSONError writes a JSON error response
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}
