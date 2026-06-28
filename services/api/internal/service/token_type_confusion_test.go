package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// TestAuthMiddleware_RejectsRefreshTokenAsBearer proves that a refresh token
// minted through the real issuance path (generateRefreshToken) cannot be
// replayed as a bearer access token against the Auth middleware. Access and
// refresh tokens share the same secret, issuer and audience, so without the
// token-type discriminator a refresh JWT authenticates every protected route
// for its full 7-day TTL — surviving logout, since the middleware never
// consults Redis. The fix pins jwt.WithSubject("access") in the middleware and
// rejects any bearer token carrying a token_id. Reverting either guard makes
// the refresh token authenticate (200) and this test fails.
func TestAuthMiddleware_RejectsRefreshTokenAsBearer(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b!")

	user := &domain.User{ID: uuid.New(), Email: "owner@example.com"}

	accessToken, err := generateAccessToken(user, secret)
	require.NoError(t, err)

	refreshToken, _, err := generateRefreshToken(user.ID, secret)
	require.NoError(t, err)

	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	handler := middleware.Auth(secret)(protected)

	call := func(bearer string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	assert.Equal(t, http.StatusUnauthorized, call(refreshToken),
		"refresh token must not authenticate as a bearer access token")

	assert.Equal(t, http.StatusOK, call(accessToken),
		"a legitimately issued access token must still authenticate")
}

// TestRefreshToken_RejectsAccessTokenSubject proves the reverse direction is
// also fenced: an access token presented to RefreshToken/Logout is rejected at
// the subject check before any Redis lookup (the service here has a nil Redis
// client, so reaching Redis would panic). With the subject guard the access
// token is rejected with domain.ErrInvalidToken and Redis is never touched;
// reverting the guard lets the access token through to the Redis lookup, which
// the recover() below surfaces as a test failure.
func TestRefreshToken_RejectsAccessTokenSubject(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b!")
	user := &domain.User{ID: uuid.New(), Email: "owner@example.com"}

	accessToken, err := generateAccessToken(user, secret)
	require.NoError(t, err)

	svc := &userService{jwtSecret: secret}

	refreshErr := callRejectingAtParse(t, func() error {
		_, _, _, e := svc.RefreshToken(t.Context(), accessToken)
		return e
	})
	require.ErrorIs(t, refreshErr, domain.ErrInvalidToken,
		"an access token must not be accepted by RefreshToken")

	logoutErr := callRejectingAtParse(t, func() error {
		return svc.Logout(t.Context(), accessToken)
	})
	require.ErrorIs(t, logoutErr, domain.ErrInvalidToken,
		"an access token must not be accepted by Logout")
}

// callRejectingAtParse runs fn and converts a panic (the symptom of an access
// token slipping past the subject check into the nil-Redis lookup) into an
// explicit non-ErrInvalidToken error, so the assertion message is readable
// instead of a raw segfault.
func callRejectingAtParse(t *testing.T, fn func() error) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("token reached Redis lookup instead of being rejected at parse: %v", r)
		}
	}()
	return fn()
}
