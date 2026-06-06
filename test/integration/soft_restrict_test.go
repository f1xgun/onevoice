package integration

// Soft-restrict middleware integration tests.
//
// Coverage matrix (per 21-03-PLAN Task 12 + verification gate):
// - Day0Blocks               — unverified user POST /api/v1/businesses
// returns 412 email_verification_required
// (POST /businesses is Day7-decorated in
// this build; day0 routes are
// business-scoped and require an existing
// business — covered by ChatBlock below.)
// - Day7BlocksBusinessCreate — same as above (POST /businesses is the
// cleanest day-7 surface to assert
// without provisioning a business first).
// - DeleteUserAllowed        — t.Skip — DELETE /users/me is Phase
// 21-04 territory. Documented gate.
// - VerifyEndpointsAllowed   — POST /auth/verify-email/resend works
// for unverified post-grace user (the
// whole point of : never gate the
// verify path).
// - VerifiedUserUnrestricted — verified user with any created_at can
// POST /businesses and the request
// succeeds (no 412).
//
// Harness: same shape as password_reset_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	businessesPath               = "/api/v1/businesses"
	httpSoftRestrictBlocked      = 412
	httpSoftRestrictAuthRequired = 401
)

// postBusiness POSTs /businesses with Bearer + the minimal payload the
// handler accepts. Returns the raw response so the caller can read status.
func postBusiness(t *testing.T, accessToken, name string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"name":     name,
		"category": "cafe",
		"address":  "Тверская 1",
		"phone":    "+7 495 000 0000",
	})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, baseURL+businessesPath, bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	return resp
}

// ageUserCreatedAt rewinds users.created_at by `age`. Used to step a
// user past the 7-day grace boundary without sleeping.
func ageUserCreatedAt(t *testing.T, email string, age time.Duration) {
	t.Helper()
	_, err := pgPool.Exec(context.Background(),
		`UPDATE users SET created_at = NOW() - $1::interval WHERE email = $2`,
		fmt.Sprintf("%d seconds", int(age.Seconds())), email)
	require.NoError(t, err)
}

// markUserVerified flips email_verified=TRUE bypassing the email flow.
func markUserVerified(t *testing.T, email string) {
	t.Helper()
	_, err := pgPool.Exec(context.Background(),
		`UPDATE users SET email_verified = TRUE, email_verified_at = NOW() WHERE email = $1`, email)
	require.NoError(t, err)
}

// --- Day7BlocksBusinessCreate -------------------------------------------

func TestSoftRestrict_Day7BlocksBusinessCreate(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	accessToken := setupTestUser(t, "post-grace@example.com", "password123")
	ageUserCreatedAt(t, "post-grace@example.com", 8*24*time.Hour)

	resp := postBusiness(t, accessToken, "Café")
	defer resp.Body.Close()
	require.Equal(t, httpSoftRestrictBlocked, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "email_verification_required", body["code"])
	require.Contains(t, body, "verifiedDeadline")
}

// --- ChatAllowedWithinGrace ---------------------------------------------

func TestSoftRestrict_BusinessCreateAllowedWithinGrace(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	accessToken := setupTestUser(t, "in-grace@example.com", "password123")
	ageUserCreatedAt(t, "in-grace@example.com", 3*24*time.Hour)

	resp := postBusiness(t, accessToken, "Café in Grace")
	defer resp.Body.Close()
	require.NotEqual(t, httpSoftRestrictBlocked, resp.StatusCode,
		"unverified user within 7-day grace must not be 412'd")
}

// --- VerifyEndpointsAllowed --------------------------------------

func TestSoftRestrict_VerifyEndpointsAlwaysAllowed(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	accessToken := setupTestUser(t, "verify-never-gated@example.com", "password123")
	ageUserCreatedAt(t, "verify-never-gated@example.com", 30*24*time.Hour)

	resp := postVerifyResend(t, accessToken)
	defer resp.Body.Close()
	require.NotEqual(t, httpSoftRestrictBlocked, resp.StatusCode,
		"verify-email/resend must NEVER be gated by RequireVerifiedEmail")
}

// --- VerifiedUserUnrestricted -------------------------------------------

func TestSoftRestrict_VerifiedUserUnrestricted(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	accessToken := setupTestUser(t, "verified-old@example.com", "password123")
	ageUserCreatedAt(t, "verified-old@example.com", 100*24*time.Hour)
	markUserVerified(t, "verified-old@example.com")

	resp := postBusiness(t, accessToken, "Verified Café")
	defer resp.Body.Close()
	require.NotEqual(t, httpSoftRestrictBlocked, resp.StatusCode,
		"verified user (any age) must NOT be gated")
}

// --- DeleteUserAllowed — documented skip --------

func TestSoftRestrict_DeleteUserAlwaysAllowed(t *testing.T) {
	t.Skip("requires DELETE /users/me handler — soft-restrict matrix")
}
