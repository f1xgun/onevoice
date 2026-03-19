package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/auth"
)

var testJWTSecret = []byte("test-secret-key-for-jwt-signing")

// Helper to create valid JWT token using typed AccessTokenClaims
func createTestToken(userID uuid.UUID, email, role string, expiry time.Duration) string {
	claims := &auth.AccessTokenClaims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Audience:  jwt.ClaimStrings{auth.TokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(testJWTSecret)
	return tokenString
}

// Test handler that accesses context values
func testHandler(t *testing.T, expectedUserID uuid.UUID, expectedEmail, expectedRole string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := GetUserID(r.Context())
		require.NoError(t, err)
		assert.Equal(t, expectedUserID, userID)

		email, err := GetUserEmail(r.Context())
		require.NoError(t, err)
		assert.Equal(t, expectedEmail, email)

		role, err := GetUserRole(r.Context())
		require.NoError(t, err)
		assert.Equal(t, expectedRole, role)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}
}

func TestAuth_ValidToken(t *testing.T) {
	userID := uuid.New()
	email := "test@example.com"
	role := "owner"

	token := createTestToken(userID, email, role, 15*time.Minute)

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)

	rr := httptest.NewRecorder()

	handler := Auth(testJWTSecret)(testHandler(t, userID, email, role))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "success", rr.Body.String())
}

func TestAuth_MissingAuthorizationHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	// No Authorization header

	rr := httptest.NewRecorder()

	handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Equal(t, "missing authorization header", errResp.Error)
}

func TestAuth_InvalidHeaderFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "some-token"},
		{"wrong prefix", "Basic some-token"},
		{"only bearer", "Bearer"},
		{"empty bearer", "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
			req.Header.Set("Authorization", tt.header)

			rr := httptest.NewRecorder()

			handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler should not be called")
			}))
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusUnauthorized, rr.Code)

			var errResp ErrorResponse
			err := json.NewDecoder(rr.Body).Decode(&errResp)
			require.NoError(t, err)
			assert.Contains(t, errResp.Error, "invalid authorization header")
		})
	}
}

func TestAuth_InvalidTokenSignature(t *testing.T) {
	userID := uuid.New()
	token := createTestToken(userID, "test@example.com", "owner", 15*time.Minute)

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)

	rr := httptest.NewRecorder()

	// Use different secret to cause signature validation failure
	wrongSecret := []byte("wrong-secret-key")
	handler := Auth(wrongSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "token_invalid")
}

func TestAuth_ExpiredToken(t *testing.T) {
	userID := uuid.New()
	// Create token that expired 1 hour ago
	token := createTestToken(userID, "test@example.com", "owner", -1*time.Hour)

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)

	rr := httptest.NewRecorder()

	handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "token")
}

func TestAuth_MalformedToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer malformed.token.here")

	rr := httptest.NewRecorder()

	handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "token_invalid")
}

func TestAuth_MissingClaims(t *testing.T) {
	tests := []struct {
		name   string
		claims jwt.MapClaims
	}{
		{
			name: "missing user_id",
			claims: jwt.MapClaims{
				"email": "test@example.com",
				"role":  "owner",
				"exp":   time.Now().Add(15 * time.Minute).Unix(),
			},
		},
		{
			name: "missing email",
			claims: jwt.MapClaims{
				"user_id": uuid.New().String(),
				"role":    "owner",
				"exp":     time.Now().Add(15 * time.Minute).Unix(),
			},
		},
		{
			name: "missing role",
			claims: jwt.MapClaims{
				"user_id": uuid.New().String(),
				"email":   "test@example.com",
				"exp":     time.Now().Add(15 * time.Minute).Unix(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, tt.claims)
			tokenString, err := token.SignedString(testJWTSecret)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
			req.Header.Set("Authorization", "Bearer "+tokenString)

			rr := httptest.NewRecorder()

			handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler should not be called")
			}))
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusUnauthorized, rr.Code)

			var errResp ErrorResponse
			err = json.NewDecoder(rr.Body).Decode(&errResp)
			require.NoError(t, err)
			assert.Contains(t, errResp.Error, "token_invalid")
		})
	}
}

func TestAuth_InvalidUserIDFormat(t *testing.T) {
	claims := jwt.MapClaims{
		"user_id": "not-a-valid-uuid",
		"email":   "test@example.com",
		"role":    "owner",
		"exp":     time.Now().Add(15 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(testJWTSecret)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	rr := httptest.NewRecorder()

	handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	var errResp ErrorResponse
	err = json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "token_invalid")
}

func TestGetUserID_Success(t *testing.T) {
	expectedID := uuid.New()
	ctx := context.WithValue(context.Background(), UserIDKey, expectedID)

	userID, err := GetUserID(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedID, userID)
}

func TestGetUserID_Missing(t *testing.T) {
	ctx := context.Background()

	_, err := GetUserID(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user_id not found")
}

func TestGetUserID_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), UserIDKey, "not-a-uuid")

	_, err := GetUserID(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")
}

func TestGetUserEmail_Success(t *testing.T) {
	expectedEmail := "test@example.com"
	ctx := context.WithValue(context.Background(), UserEmailKey, expectedEmail)

	email, err := GetUserEmail(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedEmail, email)
}

func TestGetUserEmail_Missing(t *testing.T) {
	ctx := context.Background()

	_, err := GetUserEmail(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "email not found")
}

func TestGetUserRole_Success(t *testing.T) {
	expectedRole := "owner"
	ctx := context.WithValue(context.Background(), UserRoleKey, expectedRole)

	role, err := GetUserRole(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedRole, role)
}

func TestGetUserRole_Missing(t *testing.T) {
	ctx := context.Background()

	_, err := GetUserRole(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "role not found")
}

func TestAuth_WrongSigningMethod(t *testing.T) {
	userID := uuid.New()
	claims := &auth.AccessTokenClaims{
		UserID: userID,
		Email:  "test@example.com",
		Role:   "owner",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Audience:  jwt.ClaimStrings{auth.TokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// Sign with HS384 instead of HS256
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	tokenString, err := token.SignedString(testJWTSecret)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	rr := httptest.NewRecorder()
	handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	var errResp ErrorResponse
	err = json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Equal(t, "token_invalid", errResp.Error)
}

func TestAuth_WrongIssuer(t *testing.T) {
	userID := uuid.New()
	claims := &auth.AccessTokenClaims{
		UserID: userID,
		Email:  "test@example.com",
		Role:   "owner",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "wrong-issuer",
			Audience:  jwt.ClaimStrings{auth.TokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(testJWTSecret)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	rr := httptest.NewRecorder()
	handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	var errResp ErrorResponse
	err = json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Equal(t, "token_invalid", errResp.Error)
}

func TestAuth_WrongAudience(t *testing.T) {
	userID := uuid.New()
	claims := &auth.AccessTokenClaims{
		UserID: userID,
		Email:  "test@example.com",
		Role:   "owner",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Audience:  jwt.ClaimStrings{"wrong-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(testJWTSecret)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	rr := httptest.NewRecorder()
	handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	var errResp ErrorResponse
	err = json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Equal(t, "token_invalid", errResp.Error)
}

func TestAuth_NoneAlgorithm(t *testing.T) {
	// Manually construct an unsigned JWT (alg: none)
	// Header: {"alg":"none","typ":"JWT"}
	// Claims: valid-looking claims with correct issuer/audience
	header := base64RawURL([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64RawURL([]byte(`{"user_id":"` + uuid.New().String() + `","email":"test@example.com","role":"owner","iss":"` + auth.TokenIssuer + `","aud":["` + auth.TokenAudience + `"],"exp":` + fmt.Sprintf("%d", time.Now().Add(15*time.Minute).Unix()) + `}`))
	tokenString := header + "." + payload + "."

	req := httptest.NewRequest("GET", "/api/v1/businesses", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	rr := httptest.NewRecorder()
	handler := Auth(testJWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	var errResp ErrorResponse
	err := json.NewDecoder(rr.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Equal(t, "token_invalid", errResp.Error)
}

// base64RawURL encodes bytes to base64url without padding (JWT standard encoding).
func base64RawURL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
