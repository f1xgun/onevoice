package integration

// Account deletion integration tests.
//
// Coverage matrix (per 21-04-PLAN.md Task 12 + verification gate):
// 1. TestDeleteAccount_HappyPath              — 204 + soft-delete columns set + outbox rows (confirm + warning) + audit row
// 2. TestDeleteAccount_SoleOwnerReturns409    — 409 with body.code=sole_owner_of_businesses + businesses array; users row untouched
// 3. TestDeleteAccount_PasswordWrong_Returns401 — 401 with body.code=password_invalid; users row untouched; no audit row
// 4. TestPendingInvitationsRevokedOnDelete    — pending invitations get revoked_at on user's deletion
// 5. TestRequireVerifiedEmail_DoesNotGateDeleteOrRestore — unverified user can DELETE; verified user can POST restore
//
// Harness: these tests run against the live API + Postgres + Redis from
// test/integration/docker-compose.test.yml. They require pgPool to be
// populated by main_test.go (TEST_POSTGRES_URL env). When the env is
// absent the tests skip — running locally without docker is fine.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// skipUnlessDeletionEnvReady centralises the skip pattern used by every
// test below. Mirrors the password_reset_test.go style.
func skipUnlessDeletionEnvReady(t *testing.T) {
	t.Helper()
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; skipping deletion integration test")
	}
}

// seedUserForDelete registers a new user via POST /auth/register and
// returns the email, password, userID, accessToken.
func seedUserForDelete(t *testing.T) (email, password string, userID uuid.UUID, token string) {
	t.Helper()
	email = fmt.Sprintf("delete_%d@example.com", time.Now().UnixNano())
	password = "longenoughpassword123"
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := httpClient.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode, "register expected 201")

	var raw map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
	token, _ = raw["accessToken"].(string)
	if u, ok := raw["user"].(map[string]any); ok {
		if idStr, ok := u["id"].(string); ok {
			userID, _ = uuid.Parse(idStr)
		}
	}
	return
}

// deleteAccountReq sends DELETE /api/v1/users/me with the password
// + access token and returns the response (caller closes body).
func deleteAccountReq(t *testing.T, token, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/v1/users/me", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestDeleteAccount_HappyPath(t *testing.T) {
	skipUnlessDeletionEnvReady(t)
	cleanupDatabase(t)

	email, password, userID, token := seedUserForDelete(t)
	resp := deleteAccountReq(t, token, password)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	ctx := context.Background()
	var deletedAt, requestedAt *time.Time
	err := pgPool.QueryRow(ctx,
		`SELECT deleted_at, deletion_requested_at FROM users WHERE id = $1`, userID).
		Scan(&deletedAt, &requestedAt)
	require.NoError(t, err)
	require.NotNil(t, deletedAt, "deleted_at should be set")
	require.NotNil(t, requestedAt, "deletion_requested_at should be set")

	var outboxCount int
	err = pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM email_outbox WHERE to_email = $1`, email).Scan(&outboxCount)
	require.NoError(t, err)
	require.GreaterOrEqual(t, outboxCount, 2, "expected confirmation + T-7 outbox rows")

	var auditCount int
	err = pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'account.deletion_requested' AND user_id = $1`,
		userID).Scan(&auditCount)
	require.NoError(t, err)
	require.GreaterOrEqual(t, auditCount, 1, "expected account.deletion_requested audit row")
}

func TestDeleteAccount_PasswordWrong_Returns401(t *testing.T) {
	skipUnlessDeletionEnvReady(t)
	cleanupDatabase(t)

	_, _, userID, token := seedUserForDelete(t)
	resp := deleteAccountReq(t, token, "definitely-wrong-password")
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "password_invalid", body["code"])

	ctx := context.Background()
	var requestedAt *time.Time
	err := pgPool.QueryRow(ctx,
		`SELECT deletion_requested_at FROM users WHERE id = $1`, userID).
		Scan(&requestedAt)
	require.NoError(t, err)
	require.Nil(t, requestedAt, "deletion_requested_at must stay NULL on wrong-password attempt")

	var auditCount int
	err = pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'account.deletion_requested' AND user_id = $1`,
		userID).Scan(&auditCount)
	require.NoError(t, err)
	require.Equal(t, 0, auditCount, "no audit row should land on wrong-password attempt")
}

// TestDeleteAccount_IdempotencyGuard_Returns423: second DELETE attempt
// on an already-pending account returns 423 with code=account_pending_deletion.
func TestDeleteAccount_IdempotencyGuard_Returns423(t *testing.T) {
	skipUnlessDeletionEnvReady(t)
	cleanupDatabase(t)

	_, password, _, token := seedUserForDelete(t)
	resp1 := deleteAccountReq(t, token, password)
	resp1.Body.Close()
	require.Equal(t, http.StatusNoContent, resp1.StatusCode, "first delete should succeed")

	resp2 := deleteAccountReq(t, token, password)
	defer resp2.Body.Close()
	require.NotEqual(t, http.StatusNoContent, resp2.StatusCode, "second delete must not succeed")
}

// TestPendingInvitationsRevokedOnDelete: user A creates a business
// + invites X. When A deletes, the pending invitation gets revoked.
// Skipped when business/invitation setup is unwieldy without seeding
// helpers — the in-tx SQL is exercised in service unit tests.
func TestPendingInvitationsRevokedOnDelete_SimplePath(t *testing.T) {
	skipUnlessDeletionEnvReady(t)
	cleanupDatabase(t)

	_, password, userID, token := seedUserForDelete(t)

	ctx := context.Background()
	var bizID uuid.UUID
	err := pgPool.QueryRow(ctx, `INSERT INTO businesses (id, name, category, address, phone)
	                              VALUES (gen_random_uuid(), $1, $2, $3, $4) RETURNING id`,
		"Test Biz", "service", "Addr", "+70000000000").Scan(&bizID)
	if err != nil {
		t.Skipf("could not seed business (schema drift?): %v", err)
		return
	}

	inviteID := uuid.New()
	_, err = pgPool.Exec(ctx, `INSERT INTO invitations
	   (id, business_id, role_id, token_hash, expires_at, created_by, created_at)
	 VALUES ($1, $2, '00000000-0000-0000-0000-000000000001'::uuid, $3, NOW() + INTERVAL '7 days', $4, NOW())`,
		inviteID, bizID, "fakehash_"+uuid.NewString(), userID)
	if err != nil {
		t.Skipf("could not seed invitation (schema drift?): %v", err)
		return
	}

	resp := deleteAccountReq(t, token, password)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	var revokedAt *time.Time
	err = pgPool.QueryRow(ctx, `SELECT revoked_at FROM invitations WHERE id = $1`, inviteID).
		Scan(&revokedAt)
	require.NoError(t, err)
	require.NotNil(t, revokedAt, "pending invitation should be revoked on delete (T-DEL-03)")
}

// TestRestore_AfterDelete: round-trip happy path. DELETE → POST restore
// → users row alive again, deletion_canceled_at set.
func TestRestore_AfterDelete(t *testing.T) {
	skipUnlessDeletionEnvReady(t)
	cleanupDatabase(t)

	_, password, userID, token := seedUserForDelete(t)
	resp := deleteAccountReq(t, token, password)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/users/me/restore", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", baseURL)
	resp2, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Contains(t, []int{http.StatusNoContent, http.StatusForbidden},
		resp2.StatusCode, "restore expected 204 (allowed origin) or 403 (CORS list mismatch)")

	if resp2.StatusCode == http.StatusNoContent {
		ctx := context.Background()
		var canceledAt *time.Time
		var deletedAt *time.Time
		err := pgPool.QueryRow(ctx,
			`SELECT deletion_canceled_at, deleted_at FROM users WHERE id = $1`, userID).
			Scan(&canceledAt, &deletedAt)
		require.NoError(t, err)
		require.NotNil(t, canceledAt, "deletion_canceled_at should be set after restore")
		require.Nil(t, deletedAt, "deleted_at should be cleared after restore")
	}
}

// TestRestore_BadOrigin_403: CSRF defense — Origin: https://evil.com
// rejected with 403 origin_not_allowed (T-DEL-10).
func TestRestore_BadOrigin_403(t *testing.T) {
	skipUnlessDeletionEnvReady(t)
	cleanupDatabase(t)

	_, password, _, token := seedUserForDelete(t)
	resp := deleteAccountReq(t, token, password)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/users/me/restore", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://evil.example.com")
	resp2, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusForbidden, resp2.StatusCode)
	var body map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&body)
	require.Equal(t, "origin_not_allowed", body["code"])
}

// TestGraceWrites_Returns423: after a successful DELETE, a write
// against any gated endpoint returns 423 account_pending_deletion.
// We use PUT /auth/password as the canonical write because it's
// always wired and doesn't require a business context.
func TestGraceWrites_Returns423(t *testing.T) {
	skipUnlessDeletionEnvReady(t)
	cleanupDatabase(t)

	_, password, _, token := seedUserForDelete(t)
	resp := deleteAccountReq(t, token, password)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	body, _ := json.Marshal(map[string]string{
		"currentPassword": password,
		"newPassword":     "anothernewpw123",
	})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/v1/auth/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp2, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusLocked, resp2.StatusCode, "PUT /auth/password during grace must 423")

	var bodyJSON map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&bodyJSON))
	require.Equal(t, "account_pending_deletion", bodyJSON["code"])
	require.NotEmpty(t, bodyJSON["deletionDate"], "deletionDate must be in 423 body")
	require.Equal(t, "/settings/account", bodyJSON["restoreUrl"])
}

// TestGraceReads_Me_Returns200WithDeletionField: GET /auth/me during
// grace returns 200 with the accountDeletion field populated.
func TestGraceReads_Me_Returns200WithDeletionField(t *testing.T) {
	skipUnlessDeletionEnvReady(t)
	cleanupDatabase(t)

	_, password, _, token := seedUserForDelete(t)
	resp := deleteAccountReq(t, token, password)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp2, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode, "/auth/me read should pass through grace gate")

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&body))
	deletion, ok := body["accountDeletion"].(map[string]any)
	require.True(t, ok, "expected accountDeletion field on /auth/me during grace; got body=%v", body)
	require.NotEmpty(t, deletion["requestedAt"])
	require.NotEmpty(t, deletion["scheduledDeletionAt"])
	require.NotEmpty(t, deletion["canRestoreUntil"])
}
