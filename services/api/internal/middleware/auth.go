package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
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
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				// Verify signing method
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return jwtSecret, nil
			})

			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "invalid token: "+err.Error())
				return
			}

			if !token.Valid {
				writeJSONError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			// Extract claims
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "invalid token claims")
				return
			}

			// Extract user_id
			userIDStr, ok := claims["user_id"].(string)
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "invalid token claims: missing user_id")
				return
			}

			userID, err := uuid.Parse(userIDStr)
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "invalid user_id format")
				return
			}

			// Extract email
			email, ok := claims["email"].(string)
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "invalid token claims: missing email")
				return
			}

			// Extract role
			role, ok := claims["role"].(string)
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "invalid token claims: missing role")
				return
			}

			// Store claims in context
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, UserEmailKey, email)
			ctx = context.WithValue(ctx, UserRoleKey, role)

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
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}
