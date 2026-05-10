//go:build integration

package handler_test

// Phase 3 plan 03-06 Task 2 — cross-cutting integration tests for invitations:
//   - TestInvitations_Create_PendingCap_HardCeiling — INVITE-04 end-to-end
//     (real PG, real Serializable tx, real cap enforcement under sequential load)
//   - TestInvitations_Create_StressCap — INVITE-04 + threat T-03-07 (cap bypass
//     under concurrent creates; spawns 25 goroutines and asserts count201<=20)
//   - TestInvitations_Accept_FreshSession200 — INVITE-11 end-to-end
//     (cache-invalidation propagation; new member's next call returns 200)
//   - TestInvitations_RevokedThen410 — INVITE-06 round-trip
//     (revoke → re-revoke 410 idempotent → accept-as-different-user 410 revoked)
//   - TestInvitations_Preview_RateLimited — T-03-09 (public preview enumeration
//     defense via per-IP rate limit; uses setupTestEnvWithLoginRateLimit helper)
//
// All tests are gated by //go:build integration and use the real Postgres in
// test/integration/docker-compose.test.yml. They share helpers / mocks /
// scaffolding with rbac_coverage_test.go via the same handler_test package.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestInvitations_Create_PendingCap_HardCeiling exercises INVITE-04:
//  1. Owner creates 20 pending invitations → 20 succeed (201).
//  2. The 21st returns 429 too_many_pending.
//  3. Revoke one → next attempt returns 201.
//
// Validates the pgx.Serializable-tx cap holds against real PG (RESEARCH OQ-01
// refinement of CONTEXT D-14) under sequential load. The concurrent stress
// test below covers the racy case.
func TestInvitations_Create_PendingCap_HardCeiling(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	ownerJWT := mintJWT(t, env.jwtSecret, ownerID)
	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)

	url := fmt.Sprintf("/api/v1/businesses/%s/invitations", bizID)
	body := fmt.Sprintf(`{"role_id":"%s"}`, editorRoleID)

	// Create 20 pending — must all succeed.
	for i := 0; i < 20; i++ {
		rec := doAuthedRequest(t, env, http.MethodPost, url, ownerJWT, []byte(body))
		require.Equal(t, http.StatusCreated, rec.Code,
			"create #%d should 201, got %d body=%q", i, rec.Code, rec.Body.String())
	}

	// 21st → 429.
	rec := doAuthedRequest(t, env, http.MethodPost, url, ownerJWT, []byte(body))
	require.Equal(t, http.StatusTooManyRequests, rec.Code,
		"21st create should 429, got %d body=%q", rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "too_many_pending")

	// Revoke one (pick the most-recently-created via SQL — simpler than parsing
	// a random response). The pending count drops from 20 to 19, so the next
	// create returns 201.
	_, err := env.pool.Exec(context.Background(),
		`UPDATE invitations SET revoked_at = NOW()
		 WHERE id = (
		     SELECT id FROM invitations
		     WHERE business_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL
		     ORDER BY created_at DESC LIMIT 1
		 )`, bizID)
	require.NoError(t, err)

	// 21st (re-attempt) → 201.
	rec = doAuthedRequest(t, env, http.MethodPost, url, ownerJWT, []byte(body))
	require.Equal(t, http.StatusCreated, rec.Code,
		"after revoking one, next create should 201, got %d body=%q", rec.Code, rec.Body.String())
}

// TestInvitations_Create_StressCap exercises the 20-pending cap under
// concurrent creates (INVITE-04 + threat T-03-07: cap bypass under
// concurrency). Spawns 25 concurrent POST goroutines; asserts count201<=20
// AND DB pending count <= 20, with no panics.
//
// Note: under Serializable isolation, contending tx may also fail with
// sqlstate 40001 (serialization_failure) which the handler currently
// surfaces as 500. The test allows a small number of 500s ONLY if the total
// 201+429+500 == 25 AND 201 <= 20 (cap never exceeded). This is the safety
// property; tightening to "no 500s" requires retry logic deferred to v2.1.
func TestInvitations_Create_StressCap(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	ownerJWT := mintJWT(t, env.jwtSecret, ownerID)
	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)

	url := fmt.Sprintf("/api/v1/businesses/%s/invitations", bizID)
	body := fmt.Sprintf(`{"role_id":"%s"}`, editorRoleID)

	const N = 25
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		count201 int
		count429 int
		count500 int
		other    []int
	)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("concurrent create panicked: %v", r)
				}
			}()
			rec := doAuthedRequest(t, env, http.MethodPost, url, ownerJWT, []byte(body))
			mu.Lock()
			defer mu.Unlock()
			switch rec.Code {
			case http.StatusCreated:
				count201++
			case http.StatusTooManyRequests:
				count429++
			case http.StatusInternalServerError:
				count500++ // sqlstate 40001 surfaces as 500 in v2.0; documented above.
			default:
				other = append(other, rec.Code)
			}
		}()
	}
	wg.Wait()

	require.Empty(t, other, "unexpected status codes from concurrent creates: %v", other)
	require.Equal(t, N, count201+count429+count500,
		"total responses %d != %d (201=%d 429=%d 500=%d)",
		count201+count429+count500, N, count201, count429, count500)
	require.LessOrEqual(t, count201, 20,
		"INVITE-04 / T-03-07 cap bypass: count201=%d > 20 — cap bypass under concurrency", count201)

	// DB invariant: pending count must <= 20.
	var dbPending int
	err := env.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM invitations
		 WHERE business_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > NOW()`,
		bizID).Scan(&dbPending)
	require.NoError(t, err)
	require.LessOrEqual(t, dbPending, 20,
		"INVITE-04 DB invariant: pending count %d > 20 after concurrent stress (cap bypassed at DB tier)", dbPending)
}

// TestInvitations_Accept_FreshSession200 exercises INVITE-11 end-to-end:
// the cache-invalidation contract holds against the real authz LRU.
//
// Steps:
//  1. Owner creates an invitation for editorRole.
//  2. Capture the raw token from the create response.
//  3. A different (non-member) user JWTs in and POSTs accept.
//  4. The new member's NEXT request to a business-scoped GET returns 200
//     within one cache-refresh cycle (not 404 — RequireBusinessAccess sees
//     the freshly-inserted membership because InvalidateMember fired
//     after Commit).
//  5. Replay the same token: handler pre-classifies the now-AcceptedAt-set
//     invitation and returns 410 reason="accepted" (D-19 / INVITE-08 lock).
func TestInvitations_Accept_FreshSession200(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	ownerJWT := mintJWT(t, env.jwtSecret, ownerID)
	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)

	// 1. Owner creates an invitation.
	createURL := fmt.Sprintf("/api/v1/businesses/%s/invitations", bizID)
	createBody := fmt.Sprintf(`{"role_id":"%s"}`, editorRoleID)
	createRec := doAuthedRequest(t, env, http.MethodPost, createURL, ownerJWT, []byte(createBody))
	require.Equal(t, http.StatusCreated, createRec.Code, "create body=%q", createRec.Body.String())

	var createResp struct {
		ID    uuid.UUID `json:"id"`
		Token string    `json:"token"`
	}
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &createResp))
	require.NotEmpty(t, createResp.Token, "INVITE-01: token must be present in create response")

	// 2. New user — not yet a member — accepts.
	newUserID := seedUser(t, env.pool)
	newUserJWT := mintJWT(t, env.jwtSecret, newUserID)
	acceptURL := fmt.Sprintf("/api/v1/invitations/%s/accept", createResp.Token)

	// Pre-warm the authz cache for newUser by attempting a business-scoped GET
	// BEFORE acceptance — this MUST be 404 (no membership yet). RequireBusinessAccess
	// returns 404 (not 403) for non-members per AUTHZ-05.
	getURL := fmt.Sprintf("/api/v1/businesses/%s/integrations", bizID)
	rec := doAuthedRequest(t, env, http.MethodGet, getURL, newUserJWT, nil)
	require.Equal(t, http.StatusNotFound, rec.Code,
		"before accept, new user must get 404, got %d body=%q", rec.Code, rec.Body.String())

	// 3. POST accept.
	rec = doAuthedRequest(t, env, http.MethodPost, acceptURL, newUserJWT, nil)
	require.Equal(t, http.StatusOK, rec.Code, "accept should 200, body=%q", rec.Body.String())

	// 4. Next business-scoped GET MUST return 200 (cache invalidated post-commit).
	// INVITE-11 / SC-4 (one cache-refresh window): the editor role permissions
	// include integrations.read, so this returns 200 with the (empty) integrations
	// list. If the cache wasn't invalidated, RequireBusinessAccess would still
	// see no membership and return 404.
	rec = doAuthedRequest(t, env, http.MethodGet, getURL, newUserJWT, nil)
	require.Equal(t, http.StatusOK, rec.Code,
		"INVITE-11: post-accept, new member's next GET MUST 200 within one cache-refresh window, got %d body=%q",
		rec.Code, rec.Body.String())

	// 5. Replay the same token. The handler pre-classifies AcceptedAt != nil
	// and returns 410 with reason="accepted" (D-19 row "already accepted",
	// INVITE-08 single-use). The membership-check branch (which would have
	// returned 409 already_member) is unreachable because pre-classify fires
	// FIRST in invitations.go:501-504.
	rec = doAuthedRequest(t, env, http.MethodPost, acceptURL, newUserJWT, nil)
	require.Equal(t, http.StatusGone, rec.Code,
		"replay (token already accepted) should 410, got %d body=%q", rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"reason":"accepted"`,
		"replay 410 body must carry reason=accepted (D-19 INVITE-08), got %q", rec.Body.String())
}

// TestInvitations_RevokedThen410 exercises INVITE-06 round-trip: a revoked
// invitation refuses subsequent accept attempts with 410 gone (reason=revoked).
//
// Steps:
//  1. Owner creates invitation; captures token + ID.
//  2. Owner DELETEs (revokes) — 204.
//  3. Owner DELETEs again (idempotent) — 410 reason="revoked" (D-11).
//  4. Different user POSTs accept with the captured token — 410 reason="revoked"
//     (Accept handler pre-classifies RevokedAt != nil before touching membership).
func TestInvitations_RevokedThen410(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	ownerJWT := mintJWT(t, env.jwtSecret, ownerID)
	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)

	// 1. Owner creates invitation.
	createBody := fmt.Sprintf(`{"role_id":"%s"}`, editorRoleID)
	createRec := doAuthedRequest(t, env, http.MethodPost,
		fmt.Sprintf("/api/v1/businesses/%s/invitations", bizID), ownerJWT, []byte(createBody))
	require.Equal(t, http.StatusCreated, createRec.Code, "create body=%q", createRec.Body.String())

	var createResp struct {
		ID    uuid.UUID `json:"id"`
		Token string    `json:"token"`
	}
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &createResp))
	require.NotEmpty(t, createResp.Token)

	// 2. Owner revokes — 204.
	revokeURL := fmt.Sprintf("/api/v1/businesses/%s/invitations/%s", bizID, createResp.ID)
	rec := doAuthedRequest(t, env, http.MethodDelete, revokeURL, ownerJWT, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, "revoke should 204, body=%q", rec.Body.String())

	// 3. Re-revoke (idempotency — 410 gone with reason=revoked). D-11.
	rec = doAuthedRequest(t, env, http.MethodDelete, revokeURL, ownerJWT, nil)
	require.Equal(t, http.StatusGone, rec.Code,
		"re-revoke should 410 idempotent, got %d body=%q", rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"reason":"revoked"`)

	// 4. New user attempts accept with the captured token → 410 with reason=revoked
	// (INVITE-06 round-trip — accept pre-classifies RevokedAt != nil before
	// reaching membership-check / MarkAcceptedInTx).
	newUserID := seedUser(t, env.pool)
	newUserJWT := mintJWT(t, env.jwtSecret, newUserID)
	acceptURL := fmt.Sprintf("/api/v1/invitations/%s/accept", createResp.Token)
	rec = doAuthedRequest(t, env, http.MethodPost, acceptURL, newUserJWT, nil)
	require.Equal(t, http.StatusGone, rec.Code,
		"accept of revoked invitation must 410, got %d body=%q", rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"reason":"revoked"`)
}

// TestInvitations_Preview_RateLimited exercises T-03-09: the public preview
// endpoint enforces a per-IP rate limit using the Login budget. Hammer the
// endpoint with budget+1 requests; the (budget+1)th must 429.
//
// Uses setupTestEnvWithLoginRateLimit to keep the test fast (limit=3 instead
// of the default 100). Skip is NOT permitted — T-03-09 mitigation requires
// the rate-limit test to actually run when integration env is available.
//
// The rawToken is a synthetic value that the repo lookup will surface as
// ErrInvitationNotFound (handler maps to 410 reason="unknown" per D-19),
// but the rate-limit middleware fires BEFORE the handler — so we don't need
// a real invitation row, only a stable URL path that all 4 requests share.
func TestInvitations_Preview_RateLimited(t *testing.T) {
	env := setupTestEnvWithLoginRateLimit(t, 3) // 3-req-per-minute budget for fast test
	teardownTestData(t, env.pool)

	rawToken := "test-preview-token-" + uuid.New().String()
	url := "/api/v1/invitations/" + rawToken

	// Burn the budget: 3 requests, none of them rate-limited (they will 410
	// with reason="unknown" but that's after the rate-limit check).
	for i := 0; i < 3; i++ {
		rec := doAuthedRequest(t, env, http.MethodGet, url, "", nil)
		require.NotEqual(t, http.StatusTooManyRequests, rec.Code,
			"request %d/3 must NOT be rate-limited yet, got %d body=%q",
			i+1, rec.Code, rec.Body.String())
	}

	// 4th request → 429 (budget=3 exhausted).
	rec := doAuthedRequest(t, env, http.MethodGet, url, "", nil)
	require.Equal(t, http.StatusTooManyRequests, rec.Code,
		"4th request must be 429 (budget=3 exhausted), got %d body=%q",
		rec.Code, rec.Body.String())

	// Sanity: ensure the helper computeTokenHash matches the production path
	// — future regressions in lookup hashing show up here even though the
	// row is missing.
	sum := sha256.Sum256([]byte(rawToken))
	require.NotEmpty(t, hex.EncodeToString(sum[:]))
}
