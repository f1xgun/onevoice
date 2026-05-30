package integration

// Password reset integration tests.
//
// Coverage matrix (per 21-02-PLAN.md Task 9 + verification gate):
// 1. TestPasswordReset_TimingParity — : p50/p99 delta ≤ 50ms
// between known-email and
// unknown-email branches over
// 100 iterations each. The
// load-bearing anti-enumeration
// check (PITFALLS §1.1).
// 2. TestPasswordReset_HappyPath         — request → email_outbox row →
// POST confirm → login with the
// new password (200) → login with
// the old password (401).
// 3. TestPasswordReset_TokenReuse_Defense — second confirm with the same
// token returns 400
// reset_token_invalid (atomic
// consume; PITFALLS §1.3).
// 4. TestPasswordReset_ExpiredToken_Defense — manually-aged token (UPDATE
// expires_at = NOW - 1m)
// rejected as
// reset_token_invalid.
//
// Harness: these tests run against the live API + Postgres + Redis from
// test/integration/docker-compose.test.yml. They require pgPool to be
// populated by main_test.go (TEST_POSTGRES_URL env). When the env is
// absent the tests are skipped — running locally without docker is fine.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Tunable thresholds — see TestPasswordReset_TimingParity rationale.
const (
	timingParityN         = 100
	timingParityWarmup    = 5
	timingParityMaxDelta  = 50 * time.Millisecond
	resetRequestPath      = "/api/v1/auth/password-reset/request"
	resetConfirmPath      = "/api/v1/auth/password-reset/confirm"
	httpStatusNoContentOK = 204
)

// --- HTTP helpers ------------------------------------------------------

// postReset sends POST /auth/password-reset/request {email}.
func postReset(t *testing.T, email string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email})
	require.NoError(t, err)
	resp, err := httpClient.Post(baseURL+resetRequestPath, "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	return resp
}

// postConfirm sends POST /auth/password-reset/confirm {token, newPassword}.
func postConfirm(t *testing.T, token, newPassword string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token, "newPassword": newPassword})
	require.NoError(t, err)
	resp, err := httpClient.Post(baseURL+resetConfirmPath, "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	return resp
}

// postLogin returns the raw response so callers can assert on status
// without relying on the existing test helpers' assumptions.
func postLogin(t *testing.T, email, password string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	require.NoError(t, err)
	resp, err := httpClient.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	return resp
}

// fetchLatestOutboxBody pulls the most recent email_outbox.body_text for
// the recipient. Used to extract the reset token the API emailed out.
func fetchLatestOutboxBody(t *testing.T, toEmail string) string {
	t.Helper()
	require.NotNil(t, pgPool, "pgPool must be initialized (TEST_POSTGRES_URL)")
	var body string
	err := pgPool.QueryRow(context.Background(),
		`SELECT body_text FROM email_outbox WHERE to_email = $1 ORDER BY created_at DESC LIMIT 1`,
		toEmail,
	).Scan(&body)
	require.NoError(t, err, "outbox row must exist for %s", toEmail)
	return body
}

// extractTokenFromEmailBody pulls the ?token=... value out of the
// password-reset email body. The reset URL is hard-coded to
// https://onevoice.app/auth/password-reset/confirm?token=... in
// services/api/internal/service/password_reset.go.
var tokenRegex = regexp.MustCompile(`\?token=([A-Za-z0-9_\-]+)`)

func extractTokenFromEmailBody(t *testing.T, body string) string {
	t.Helper()
	m := tokenRegex.FindStringSubmatch(body)
	require.NotNil(t, m, "could not find ?token=... in email body")
	require.Len(t, m, 2)
	return m[1]
}

// decodeJSON pulls a JSON object response into a map.
func decodeJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	out := map[string]any{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

// fetchAuditActionsForEmail returns the audit_logs.action strings for any
// row whose details JSONB attempted_email matches the input. This works
// for BOTH the known-email branch (where details.* may include email)
// and the unknown-email branch (where the dummy row carries
// attempted_email).
func fetchAuditActionsForEmail(t *testing.T, email string) []string {
	t.Helper()
	require.NotNil(t, pgPool)
	rows, err := pgPool.Query(context.Background(),
		`SELECT action FROM audit_logs WHERE details->>'attempted_email' = $1 OR details->>'email' = $1 ORDER BY created_at ASC`,
		email,
	)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

// fetchAuditActionsForUser returns actions for rows where user_id matches.
func fetchAuditActionsForUser(t *testing.T, userID string) []string {
	t.Helper()
	require.NotNil(t, pgPool)
	rows, err := pgPool.Query(context.Background(),
		`SELECT action FROM audit_logs WHERE user_id = $1 ORDER BY created_at ASC`,
		userID,
	)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

// fetchUserIDByEmail returns the users.id for the given email.
func fetchUserIDByEmail(t *testing.T, email string) string {
	t.Helper()
	require.NotNil(t, pgPool)
	var id string
	require.NoError(t, pgPool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = $1`,
		email,
	).Scan(&id))
	return id
}

// cleanupPasswordReset wipes the rows touches between tests so
// each test starts from a clean slate. cleanupDatabase (in main_test.go)
// only TRUNCATEs the legacy 3 tables.
func cleanupPasswordReset(t *testing.T) {
	t.Helper()
	if pgPool != nil {
		_, err := pgPool.Exec(context.Background(),
			`TRUNCATE password_reset_tokens, email_outbox, audit_logs CASCADE`)
		if err != nil {
			t.Logf("warn: cleanupPasswordReset: %v", err)
		}
	}
	cleanupDatabase(t) // users + businesses + Redis flush
}

// --- Test 1: TIMING PARITY --------------------------------------

func TestPasswordReset_TimingParity(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupPasswordReset(t)
	_ = setupTestUser(t, "known@example.com", "validpassword123")

	// Warm up the connection pool + JIT so the first iteration's bcrypt
	// path doesn't skew p50.
	for i := 0; i < timingParityWarmup; i++ {
		_ = postReset(t, "known@example.com")
		_ = postReset(t, fmt.Sprintf("warmup-%d@example.com", i))
	}

	knownDurations := make([]time.Duration, 0, timingParityN)
	unknownDurations := make([]time.Duration, 0, timingParityN)

	for i := 0; i < timingParityN; i++ {
		start := time.Now()
		res := postReset(t, "known@example.com")
		require.Equal(t, httpStatusNoContentOK, res.StatusCode)
		res.Body.Close()
		knownDurations = append(knownDurations, time.Since(start))

		// Rotate the unknown email each iteration so a single rate-limit
		// counter doesn't dominate either branch.
		unknownEmail := fmt.Sprintf("missing-%d@example.com", i)
		start = time.Now()
		res = postReset(t, unknownEmail)
		require.Equal(t, httpStatusNoContentOK, res.StatusCode)
		res.Body.Close()
		unknownDurations = append(unknownDurations, time.Since(start))
	}

	knownP50, knownP99 := percentile(knownDurations, 0.50), percentile(knownDurations, 0.99)
	unknownP50, unknownP99 := percentile(unknownDurations, 0.50), percentile(unknownDurations, 0.99)
	deltaP50 := absDuration(knownP50 - unknownP50)
	deltaP99 := absDuration(knownP99 - unknownP99)

	t.Logf("known   p50=%v p99=%v", knownP50, knownP99)
	t.Logf("unknown p50=%v p99=%v", unknownP50, unknownP99)
	t.Logf("delta   p50=%v p99=%v (cap %v)", deltaP50, deltaP99, timingParityMaxDelta)

	// p99 must be human-imperceptible AND survive CI noise; 50ms is the
	// floor that achieves both. If CI flakes, raise to 75ms; do NOT lower
	// below 30ms (creates false sense of security).
	require.LessOrEqual(t, deltaP50, timingParityMaxDelta,
		"p50 timing delta exceeds cap — email enumeration possible")
	require.LessOrEqual(t, deltaP99, timingParityMaxDelta,
		"p99 timing delta exceeds cap — email enumeration possible")
}

// --- Test 2: HAPPY PATH ------------------------------------------------

func TestPasswordReset_HappyPath(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupPasswordReset(t)
	_ = setupTestUser(t, "alice@example.com", "oldpassword123")

	// 1. Request reset.
	res := postReset(t, "alice@example.com")
	require.Equal(t, httpStatusNoContentOK, res.StatusCode)
	res.Body.Close()

	// 2. Pull token from email_outbox.
	body := fetchLatestOutboxBody(t, "alice@example.com")
	token := extractTokenFromEmailBody(t, body)
	require.NotEmpty(t, token)

	// 3. Confirm with new password.
	res = postConfirm(t, token, "newpassword456")
	require.Equal(t, httpStatusNoContentOK, res.StatusCode)
	res.Body.Close()

	// 4. Login with new password — succeeds.
	loginRes := postLogin(t, "alice@example.com", "newpassword456")
	require.Equal(t, http.StatusOK, loginRes.StatusCode)
	loginRes.Body.Close()

	// 5. Login with old password — fails.
	loginOldRes := postLogin(t, "alice@example.com", "oldpassword123")
	require.Equal(t, http.StatusUnauthorized, loginOldRes.StatusCode)
	loginOldRes.Body.Close()

	// 6. Audit log has the right two actions.
	userID := fetchUserIDByEmail(t, "alice@example.com")
	actions := fetchAuditActionsForUser(t, userID)
	require.Contains(t, actions, "auth.password_reset_requested")
	require.Contains(t, actions, "auth.password_reset_completed")
}

// --- Test 3: TOKEN REUSE DEFENSE (PITFALLS §1.3) -----------------------

func TestPasswordReset_TokenReuse_Defense(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupPasswordReset(t)
	_ = setupTestUser(t, "bob@example.com", "oldpassword123")
	res := postReset(t, "bob@example.com")
	require.Equal(t, httpStatusNoContentOK, res.StatusCode)
	res.Body.Close()
	token := extractTokenFromEmailBody(t, fetchLatestOutboxBody(t, "bob@example.com"))

	// First consume — succeeds.
	res = postConfirm(t, token, "newpassword456")
	require.Equal(t, httpStatusNoContentOK, res.StatusCode)
	res.Body.Close()

	// Second consume — must fail with reset_token_invalid.
	res = postConfirm(t, token, "anotherpassword789")
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	body := decodeJSON(t, res)
	require.Equal(t, "reset_token_invalid", body["code"],
		"second consume must surface as reset_token_invalid (atomic UPDATE…RETURNING)")
}

// --- Test 4: EXPIRY DEFENSE (PITFALLS §1.1) ----------------------------

func TestPasswordReset_ExpiredToken_Defense(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupPasswordReset(t)
	_ = setupTestUser(t, "carol@example.com", "oldpassword123")
	res := postReset(t, "carol@example.com")
	require.Equal(t, httpStatusNoContentOK, res.StatusCode)
	res.Body.Close()
	token := extractTokenFromEmailBody(t, fetchLatestOutboxBody(t, "carol@example.com"))

	// Manually expire the token in-place.
	hash := sha256.Sum256([]byte(token))
	_, err := pgPool.Exec(context.Background(),
		`UPDATE password_reset_tokens SET expires_at = NOW() - INTERVAL '1 minute' WHERE token_hash = $1`,
		hash[:])
	require.NoError(t, err)

	res = postConfirm(t, token, "newpassword456")
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	body := decodeJSON(t, res)
	require.Equal(t, "reset_token_invalid", body["code"],
		"expired token must collapse to reset_token_invalid per PITFALLS §1.1 (no enumeration of failure mode)")
}

// --- Auxiliary statistics helpers --------------------------------------

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// fetchAuditActionsForEmail and mustAuditAvailable are reachable from the
// known-email branch tests when the integration env is up; kept here as
// reachable helpers.
var _ = fetchAuditActionsForEmail
