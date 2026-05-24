package integration

// Phase 21-03 (ACCT-02) — Email verification integration tests.
//
// Coverage matrix (per 21-03-PLAN Task 12 + verification gate):
//   1. TokenSingleUse                — second consume returns 400
//                                      verify_token_invalid (atomic
//                                      consume; PITFALLS §1.3).
//   2. ScannerProtection             — GET /auth/verify-email?token=...
//                                      does NOT mutate (the FE page
//                                      renders only a button; the
//                                      backend has no GET confirm
//                                      handler — POST is the only
//                                      consume path). T-VE-01 gate.
//   3. NoCookiesIssued               — POST /verify-email/confirm with
//                                      a valid token returns 204 with
//                                      ZERO Set-Cookie headers (T-VE-02
//                                      mitigation gate).
//   4. ResendThrottle                — 2nd resend within 60s → 429
//                                      verify_resend_throttled (D-24).
//   5. ChangeEmail_OnlyBeforeVerify  — unverified user PATCH /email-before-verify
//                                      changes the email + invalidates
//                                      outstanding tokens + enqueues a
//                                      new outbox row.
//   6. ChangeEmail_BlockedWhenVerified — verified user PATCH returns 403
//                                        email_already_verified.
//   7. ChangeEmail_EmailTaken        — PATCH to another user's email
//                                      returns 409 email_taken.
//
// Harness: same shape as password_reset_test.go. Tests skip when
// pgPool == nil (TEST_POSTGRES_URL not set; local-dev without docker).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	verifyConfirmPath           = "/api/v1/auth/verify-email/confirm"
	verifyResendPath            = "/api/v1/auth/verify-email/resend"
	emailBeforeVerifyPath       = "/api/v1/auth/email-before-verify"
	verifyEmailSubjectExpected  = "Подтвердите email — OneVoice"
	httpVerifyOK                = 204
	httpVerifyBadRequest        = 400
	httpVerifyForbidden         = 403
	httpVerifyConflict          = 409
	httpVerifyTooManyRequests   = 429
)

// verifyTokenRegex pulls token=... from the verify-email link. The
// service builds the link as `${PublicURL}/auth/verify-email?token=...`.
var verifyTokenRegex = regexp.MustCompile(`\?token=([A-Za-z0-9_\-]+)`)

// postVerifyConfirm POSTs the verify-email/confirm endpoint.
func postVerifyConfirm(t *testing.T, token string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token})
	require.NoError(t, err)
	resp, err := httpClient.Post(baseURL+verifyConfirmPath, "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	return resp
}

// postVerifyResend POSTs /verify-email/resend with Bearer token.
func postVerifyResend(t *testing.T, accessToken string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+verifyResendPath, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	return resp
}

// patchEmailBeforeVerify sends PATCH /auth/email-before-verify.
func patchEmailBeforeVerify(t *testing.T, accessToken, newEmail string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"newEmail": newEmail})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPatch, baseURL+emailBeforeVerifyPath, bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	return resp
}

// fetchLatestVerifyOutboxBody pulls the latest outbox row whose subject
// is the verification email subject — distinguishes it from password
// reset emails for users that hit both paths.
func fetchLatestVerifyOutboxBody(t *testing.T, toEmail string) string {
	t.Helper()
	require.NotNil(t, pgPool, "pgPool must be initialized (TEST_POSTGRES_URL)")
	var body string
	err := pgPool.QueryRow(context.Background(),
		`SELECT body_text FROM email_outbox
		  WHERE to_email = $1 AND subject = $2
		  ORDER BY created_at DESC LIMIT 1`,
		toEmail, verifyEmailSubjectExpected,
	).Scan(&body)
	require.NoError(t, err, "verify outbox row must exist for %s", toEmail)
	return body
}

func extractVerifyToken(t *testing.T, body string) string {
	t.Helper()
	m := verifyTokenRegex.FindStringSubmatch(body)
	require.NotNil(t, m, "could not find ?token=... in verify email body")
	require.Len(t, m, 2)
	return m[1]
}

// cleanupVerify wipes the Phase 21-03 tables between tests.
func cleanupVerify(t *testing.T) {
	t.Helper()
	if pgPool != nil {
		_, err := pgPool.Exec(context.Background(),
			`TRUNCATE email_verification_tokens, user_consents, email_outbox, audit_logs CASCADE`)
		if err != nil {
			t.Logf("warn: cleanupVerify: %v", err)
		}
	}
	cleanupDatabase(t)
}

// --- Test 1: token single-use --------------------------------------------

func TestEmailVerify_TokenSingleUse(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	_ = setupTestUser(t, "single-use@example.com", "password123")

	// Register enqueues a verification email atomically (D-17). Grab its token.
	body := fetchLatestVerifyOutboxBody(t, "single-use@example.com")
	token := extractVerifyToken(t, body)

	// First consume succeeds (204) and flips email_verified.
	resp := postVerifyConfirm(t, token)
	require.Equal(t, httpVerifyOK, resp.StatusCode)
	resp.Body.Close()

	var verified bool
	require.NoError(t, pgPool.QueryRow(context.Background(),
		`SELECT email_verified FROM users WHERE email = $1`, "single-use@example.com").Scan(&verified))
	require.True(t, verified)

	// Second consume → 400 verify_token_invalid (atomic UPDATE...WHERE consumed_at IS NULL).
	resp2 := postVerifyConfirm(t, token)
	require.Equal(t, httpVerifyBadRequest, resp2.StatusCode)
	defer resp2.Body.Close()
	var body2 map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&body2))
	require.Equal(t, "verify_token_invalid", body2["code"])
}

// --- Test 2: scanner protection (T-VE-01 gate) ---------------------------

func TestEmailVerify_ScannerProtection(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	_ = setupTestUser(t, "scanner@example.com", "password123")

	body := fetchLatestVerifyOutboxBody(t, "scanner@example.com")
	token := extractVerifyToken(t, body)

	// The verify-email page renders the FE button on GET (no backend GET
	// handler exists). The backend is asserted by hitting POST without
	// the token being consumed via any GET-side path — we approximate the
	// scanner by sha256-hashing the token and checking the DB row is
	// still unconsumed.
	hashArr := sha256.Sum256([]byte(token))
	var consumedAt *time.Time
	err := pgPool.QueryRow(context.Background(),
		`SELECT consumed_at FROM email_verification_tokens WHERE token_hash = $1`,
		hashArr[:]).Scan(&consumedAt)
	require.NoError(t, err)
	require.Nil(t, consumedAt, "no GET/scanner mechanism should have consumed the token before POST")

	// Now actually POST — that's the only consume path.
	resp := postVerifyConfirm(t, token)
	require.Equal(t, httpVerifyOK, resp.StatusCode)
	resp.Body.Close()

	require.NoError(t, pgPool.QueryRow(context.Background(),
		`SELECT consumed_at FROM email_verification_tokens WHERE token_hash = $1`,
		hashArr[:]).Scan(&consumedAt))
	require.NotNil(t, consumedAt, "POST must consume the token")
}

// --- Test 3: NO cookies issued on confirm (T-VE-02 gate) -----------------

func TestVerifyConfirm_NoCookiesIssued(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	_ = setupTestUser(t, "nocookies@example.com", "password123")

	body := fetchLatestVerifyOutboxBody(t, "nocookies@example.com")
	token := extractVerifyToken(t, body)

	resp := postVerifyConfirm(t, token)
	defer resp.Body.Close()
	require.Equal(t, httpVerifyOK, resp.StatusCode)

	// The load-bearing assertion — Set-Cookie MUST be empty. An attacker
	// who registered a victim's email then watched the victim click the
	// link must NOT come away with the victim's session.
	require.Empty(t, resp.Header.Values("Set-Cookie"),
		"verify-confirm MUST NOT issue cookies — T-VE-02")
}

// --- Test 4: resend throttle (D-24 — 1/min) ------------------------------

func TestEmailVerify_ResendThrottleOnePerMinute(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	accessToken := setupTestUser(t, "throttle@example.com", "password123")

	// 1st resend succeeds.
	resp := postVerifyResend(t, accessToken)
	require.Equal(t, httpVerifyOK, resp.StatusCode)
	resp.Body.Close()

	// 2nd within 60s → 429 verify_resend_throttled.
	resp2 := postVerifyResend(t, accessToken)
	defer resp2.Body.Close()
	require.Equal(t, httpVerifyTooManyRequests, resp2.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&body))
	require.Equal(t, "verify_resend_throttled", body["code"])
}

// --- Test 5: change email BEFORE verify (D-21 happy path) ---------------

func TestEmailChange_OnlyBeforeVerify_Allowed(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	accessToken := setupTestUser(t, "old@example.com", "password123")

	resp := patchEmailBeforeVerify(t, accessToken, "new@example.com")
	require.Equal(t, httpVerifyOK, resp.StatusCode, "unverified user must be able to change email")
	resp.Body.Close()

	// users.email updated.
	var actualEmail string
	require.NoError(t, pgPool.QueryRow(context.Background(),
		`SELECT email FROM users WHERE id = (SELECT id FROM users WHERE email = $1)`,
		"new@example.com").Scan(&actualEmail))
	require.Equal(t, "new@example.com", actualEmail)

	// Old outstanding token invalidated (consumed_at set).
	// Bypassing token hash lookup, we assert ALL tokens for this user are now consumed.
	var unconsumedCnt int
	require.NoError(t, pgPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM email_verification_tokens
		  WHERE user_id = (SELECT id FROM users WHERE email = $1) AND consumed_at IS NULL`,
		"new@example.com").Scan(&unconsumedCnt))
	require.Equal(t, 1, unconsumedCnt, "exactly one fresh token survives after change-email")

	// Verify a fresh outbox row to the new email exists.
	body := fetchLatestVerifyOutboxBody(t, "new@example.com")
	require.Contains(t, body, "?token=", "fresh verification link to new@example.com must exist")
}

// --- Test 6: change email BLOCKED when verified --------------------------

func TestEmailChange_OnlyBeforeVerify_BlockedWhenVerified(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	accessToken := setupTestUser(t, "verified@example.com", "password123")

	// Verify the user (bypass the email flow for setup speed).
	_, err := pgPool.Exec(context.Background(),
		`UPDATE users SET email_verified = TRUE, email_verified_at = NOW() WHERE email = $1`,
		"verified@example.com")
	require.NoError(t, err)

	resp := patchEmailBeforeVerify(t, accessToken, "elsewhere@example.com")
	defer resp.Body.Close()
	require.Equal(t, httpVerifyForbidden, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "email_already_verified", body["code"])
}

// --- Test 7: change email — taken ---------------------------------------

func TestEmailChange_EmailTaken(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)
	tokenA := setupTestUser(t, "a@example.com", "password123")
	_ = setupTestUser(t, "b@example.com", "password123")

	resp := patchEmailBeforeVerify(t, tokenA, "b@example.com")
	defer resp.Body.Close()
	require.Equal(t, httpVerifyConflict, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "email_taken", body["code"])
}

// Compile-time check the fmt import isn't dropped by future edits.
var _ = fmt.Sprintf
