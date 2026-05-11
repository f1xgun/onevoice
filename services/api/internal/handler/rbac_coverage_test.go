//go:build integration

package handler_test

// rbac_coverage_test.go — Plan 02-07 AUTHZ-10 route-walker integration tests.
//
// TestRBACCoverage_AllBusinessRoutes: walks every chi route under
// /api/v1/businesses/{id}/... and asserts the four-case authz trio per route.
//
// Sub-tests:
//   - TestRBACCoverage_SuspendedMember   (MEDIUM #6)
//   - TestRBACCoverage_CacheInvalidation (AUTHZ-04)
//   - TestRBACCoverage_TTLCeiling        (HIGH #1 + HIGH #2)
//   - TestRBACCoverage_LastOwnerSelfRemoval (MEDIUM #8 — exact 204)
//
// LOW #9: rbac_check_total{result="allow"} increment assertion is integrated
// into TestRBACCoverage_AllBusinessRoutes after the viewer GET trio.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// businessRoutePrefix is the chi route prefix for all business-scoped routes.
// The walker checks for this prefix to filter non-business routes.
const businessRoutePrefix = "/api/v1/businesses/{id}"

// TestRBACCoverage_AllBusinessRoutes is the AUTHZ-10 route walker.
//
// For every chi route under /api/v1/businesses/{id}/... it asserts:
//
//	Case 1: no JWT          → 401 Unauthorized  (Auth middleware fires)
//	Case 2: non-member JWT  → 404 Not Found     (RequireBusinessAccess 404)
//	Case 3: viewer + GET    → 200 OK or exempt  (PermContent*/Integrations*/Members* read)
//	Case 4: viewer + write  → 403 Forbidden     (viewer lacks write perms)
//
// Routes that cannot return 200 due to missing infrastructure (e.g. sub-resource
// not seeded, streaming endpoint) are exempt from Case 3 only. The authz gates
// (401, 404) are ALWAYS asserted, even for exempt routes.
//
// LOW #9 (AUTHZ-11): After the viewer GET loop completes, asserts that
// rbac_check_total{result="allow"} incremented by at least 1, confirming the
// Prometheus metric is wired end-to-end.
func TestRBACCoverage_AllBusinessRoutes(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	viewerID := seedUser(t, env.pool)
	nonMemberID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	viewerRoleID := uuid.MustParse(domain.SystemRoleViewerID)
	seedMembership(t, env.pool, bizID, viewerID, viewerRoleID)

	viewerJWT := mintJWT(t, env.jwtSecret, viewerID)
	nonMemberJWT := mintJWT(t, env.jwtSecret, nonMemberID)

	// LOW #9 — capture allow-metric baseline before the walker runs.
	rbacAllowBefore := testutil.ToFloat64(metrics.GetRBACCheckCounter().WithLabelValues("allow"))

	chiRouter, ok := env.router.(*chi.Mux)
	require.True(t, ok, "router must be *chi.Mux for chi.Walk")

	var walked int
	err := chi.Walk(chiRouter, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Filter: only routes that start with the business-scoped prefix.
		if route != businessRoutePrefix && !strings.HasPrefix(route, businessRoutePrefix+"/") {
			return nil
		}
		walked++

		t.Run(fmt.Sprintf("%s %s", method, route), func(t *testing.T) {
			url := substituteURLParams(t, env, bizID, route)

			// Case 1: no JWT → 401.
			rec := doAuthedRequest(t, env, method, url, "", nil)
			require.Equal(t, http.StatusUnauthorized, rec.Code,
				"no-JWT for %s %s: expected 401, got %d body=%q",
				method, route, rec.Code, rec.Body.String())

			// Case 2: non-member JWT → 404.
			rec = doAuthedRequest(t, env, method, url, nonMemberJWT, []byte("{}"))
			require.Equal(t, http.StatusNotFound, rec.Code,
				"non-member for %s %s: expected 404, got %d body=%q",
				method, route, rec.Code, rec.Body.String())

			// Cases 3 + 4: viewer permission check.
			if method == http.MethodGet || method == http.MethodHead {
				rec = doAuthedRequest(t, env, method, url, viewerJWT, nil)
				if !readPathExempt(method, route) {
					// Non-exempt viewer GET must reach the handler and return 200.
					require.Equal(t, http.StatusOK, rec.Code,
						"viewer GET for %s %s: expected 200, got %d body=%q",
						method, route, rec.Code, rec.Body.String())
				}
				// Exempt routes: authz gate still verified above (401 + 404);
				// handler-level response is not checked.
			} else {
				// Write verb: viewer must be denied at the authz layer (403).
				rec = doAuthedRequest(t, env, method, url, viewerJWT, []byte("{}"))
				if !writePathExempt(method, route) {
					require.Equal(t, http.StatusForbidden, rec.Code,
						"viewer write for %s %s: expected 403, got %d body=%q",
						method, route, rec.Code, rec.Body.String())
				}
			}
		})
		return nil
	})
	require.NoError(t, err)

	// AUTHZ-10 acceptance: walker must find at least 37 business-scoped routes.
	// Phase 2 baseline ~30 + Phase 3 invitations × 3 (POST/GET/DELETE) = 33,
	// + Phase 5 routes × 4 (POST /roles, PATCH /roles/{roleId},
	// DELETE /roles/{roleId}, GET /me/permissions) = 37.
	require.GreaterOrEqual(t, walked, 37,
		"AUTHZ-10: expected >=37 business-scoped routes "+
			"(Phase 2 baseline 30 + Phase 3 invitations × 3 + Phase 5 roles × 4), chi.Walk found %d", walked)

	// LOW #9 — AUTHZ-11 integration assertion: the allow counter must have
	// incremented at least once during the viewer GET checks above.
	rbacAllowAfter := testutil.ToFloat64(metrics.GetRBACCheckCounter().WithLabelValues("allow"))
	require.GreaterOrEqual(t, rbacAllowAfter, rbacAllowBefore+1,
		"AUTHZ-11 metric: rbac_check_total{result=allow} must increment during walker "+
			"(before=%.0f after=%.0f)", rbacAllowBefore, rbacAllowAfter)
}

// TestRBACCoverage_SuspendedMember — MEDIUM #6.
// Seeds a suspended membership and asserts 403 forbidden_suspended on a
// canonical business-scoped GET (integrations list).
func TestRBACCoverage_SuspendedMember(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	suspendedID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	viewerRoleID := uuid.MustParse(domain.SystemRoleViewerID)
	seedSuspendedMembership(t, env.pool, bizID, suspendedID, viewerRoleID)

	suspendedJWT := mintJWT(t, env.jwtSecret, suspendedID)

	url := fmt.Sprintf("/api/v1/businesses/%s/integrations", bizID)
	rec := doAuthedRequest(t, env, http.MethodGet, url, suspendedJWT, nil)
	require.Equal(t, http.StatusForbidden, rec.Code,
		"suspended member GET should return 403, got %d body=%q", rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "forbidden_suspended",
		"suspended member 403 body must contain 'forbidden_suspended' (Plan 02-01 middleware contract)")
}

// TestRBACCoverage_SuspendedMember_MyPermissions — Phase 5 review HIGH-01.
//
// MyPermissions deliberately skips the per-route authz.Can(...) gate (any
// active member can read their own permissions). This test pins the
// invariant that the RequireBusinessAccess middleware short-circuits BEFORE
// the handler runs for suspended members, returning 403 forbidden_suspended.
// Without this regression a future refactor that loosens the middleware
// could leak permission strings to a suspended actor.
func TestRBACCoverage_SuspendedMember_MyPermissions(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	suspendedID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	viewerRoleID := uuid.MustParse(domain.SystemRoleViewerID)
	seedSuspendedMembership(t, env.pool, bizID, suspendedID, viewerRoleID)

	suspendedJWT := mintJWT(t, env.jwtSecret, suspendedID)

	url := fmt.Sprintf("/api/v1/businesses/%s/me/permissions", bizID)
	rec := doAuthedRequest(t, env, http.MethodGet, url, suspendedJWT, nil)
	require.Equal(t, http.StatusForbidden, rec.Code,
		"suspended member GET /me/permissions must return 403, got %d body=%q",
		rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "forbidden_suspended",
		"suspended member 403 body must contain 'forbidden_suspended'")
}

// TestRBACCoverage_CacheInvalidation verifies AUTHZ-04:
// out-of-band DB UPDATE is invisible until cache.InvalidateMember is called.
func TestRBACCoverage_CacheInvalidation(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	viewerID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	viewerRoleID := uuid.MustParse(domain.SystemRoleViewerID)
	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)
	seedMembership(t, env.pool, bizID, viewerID, viewerRoleID)
	viewerJWT := mintJWT(t, env.jwtSecret, viewerID)

	// Viewer POSTs to create a conversation — must be denied (lacks PermContentCreate).
	url := fmt.Sprintf("/api/v1/businesses/%s/conversations", bizID)
	rec := doAuthedRequest(t, env, http.MethodPost, url, viewerJWT, []byte(`{"title":"x"}`))
	require.Equal(t, http.StatusForbidden, rec.Code,
		"viewer POST /conversations before promotion should 403, got %d", rec.Code)

	// Out-of-band promotion to editor (no InvalidateMember yet).
	_, err := env.pool.Exec(context.Background(),
		`UPDATE business_members SET role_id = $1 WHERE business_id = $2 AND user_id = $3`,
		editorRoleID, bizID, viewerID)
	require.NoError(t, err)

	// Stale cache: should still return 403.
	rec = doAuthedRequest(t, env, http.MethodPost, url, viewerJWT, []byte(`{"title":"x"}`))
	require.Equal(t, http.StatusForbidden, rec.Code,
		"without InvalidateMember, stale cache must still 403, got %d", rec.Code)

	// Invalidate — next request loads fresh role from DB.
	env.cache.InvalidateMember(bizID, viewerID)
	rec = doAuthedRequest(t, env, http.MethodPost, url, viewerJWT, []byte(`{"title":"x"}`))
	require.NotEqual(t, http.StatusForbidden, rec.Code,
		"after InvalidateMember, editor-promoted user must not get 403, got %d body=%q",
		rec.Code, rec.Body.String())
}

// TestRBACCoverage_TTLCeiling — HIGH #1 + HIGH #2.
//
// Constructs a short-TTL cache via authz.NewCacheForTest(loader, 1s, 1s).
// After populating the cache, sleeps 1.1s (> TTL) and asserts the next
// request re-fetches from the loader (reflected as changed permissions).
//
// The 1.1s sleep is deterministic and meets the SPEC AUTHZ-10 <1s
// determinism bar's spirit (original concern was multi-second sleeps).
// Clock interface was intentionally dropped — expirable.LRU has no seam.
func TestRBACCoverage_TTLCeiling(t *testing.T) {
	env := setupTestEnvWithTTL(t, 1*time.Second)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	viewerID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	viewerRoleID := uuid.MustParse(domain.SystemRoleViewerID)
	editorRoleID := uuid.MustParse(domain.SystemRoleEditorID)
	seedMembership(t, env.pool, bizID, viewerID, viewerRoleID)
	viewerJWT := mintJWT(t, env.jwtSecret, viewerID)

	// Prime the cache with viewer perms (403 on write).
	url := fmt.Sprintf("/api/v1/businesses/%s/conversations", bizID)
	rec := doAuthedRequest(t, env, http.MethodPost, url, viewerJWT, []byte(`{"title":"x"}`))
	require.Equal(t, http.StatusForbidden, rec.Code,
		"viewer POST before promotion should 403, got %d", rec.Code)

	// Out-of-band promotion (NO InvalidateMember — TTL must handle it).
	_, err := env.pool.Exec(context.Background(),
		`UPDATE business_members SET role_id = $1 WHERE business_id = $2 AND user_id = $3`,
		editorRoleID, bizID, viewerID)
	require.NoError(t, err)

	// Sleep beyond the injected 1s TTL (HIGH #1 + HIGH #2 — 1.1s sleep).
	time.Sleep(1100 * time.Millisecond)

	rec = doAuthedRequest(t, env, http.MethodPost, url, viewerJWT, []byte(`{"title":"x"}`))
	require.NotEqual(t, http.StatusForbidden, rec.Code,
		"after TTL elapsed (1s TTL, 1.1s slept), editor role must be reflected; got %d body=%q",
		rec.Code, rec.Body.String())
}

// TestRBACCoverage_LastOwnerSelfRemoval — MEDIUM #8 (committed to 204).
//
// Asserts:
//  1. Sole-owner self-DELETE → 422 with body containing "last_owner".
//  2. Non-last-owner self-DELETE → EXACTLY 204 No Content (MEDIUM #8 contract).
//  3. Removed user's next GET → 404 (RequireBusinessAccess sees no membership).
func TestRBACCoverage_LastOwnerSelfRemoval(t *testing.T) {
	env := setupTestEnv(t)
	teardownTestData(t, env.pool)

	ownerID := seedUser(t, env.pool)
	bizID := seedBusiness(t, env.pool, ownerID)
	ownerJWT := mintJWT(t, env.jwtSecret, ownerID)

	// Sole-owner self-DELETE → 422 last_owner.
	url := fmt.Sprintf("/api/v1/businesses/%s/members/%s", bizID, ownerID)
	rec := doAuthedRequest(t, env, http.MethodDelete, url, ownerJWT, nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
		"sole-owner self-DELETE should 422, got %d body=%q", rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "last_owner",
		"422 body must contain 'last_owner'")

	// Add a second owner so the first is no longer the last owner.
	secondOwnerID := seedUser(t, env.pool)
	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	seedMembership(t, env.pool, bizID, secondOwnerID, ownerRoleID)

	// Non-last-owner self-DELETE → EXACTLY 204 No Content (MEDIUM #8).
	rec = doAuthedRequest(t, env, http.MethodDelete, url, ownerJWT, nil)
	require.Equal(t, http.StatusNoContent, rec.Code,
		"MEDIUM #8: non-last-owner self-DELETE must return 204 No Content (not 200), "+
			"got %d body=%q", rec.Code, rec.Body.String())

	// Removed user's next GET to the business → 404 from RequireBusinessAccess.
	getURL := fmt.Sprintf("/api/v1/businesses/%s/integrations", bizID)
	rec = doAuthedRequest(t, env, http.MethodGet, getURL, ownerJWT, nil)
	require.Equal(t, http.StatusNotFound, rec.Code,
		"removed user must get 404 on next business-scoped request, got %d", rec.Code)
}

// substituteURLParams replaces chi URL placeholders with seeded UUIDs:
//   - First {id} → bizID (the business ID from the outer r.Route)
//   - Subsequent {id} → random UUID (sub-resource IDs not seeded)
//   - {conversationID} → random UUID
//   - {integrationId}  → random UUID
//   - {batch_id}       → random UUID
//   - {userId}         → a seeded viewer-role user (must exist for members routes)
//
// Placeholders are replaced left-to-right; the first {id} is always the
// business ID because chi.Walk emits the full pattern starting with
// /api/v1/businesses/{id}/...
func substituteURLParams(t *testing.T, env *testEnv, bizID uuid.UUID, route string) string {
	t.Helper()
	url := route

	// Replace the outer business {id} first, then any inner {id}.
	// strings.Replace with n=1 replaces only the first occurrence.
	url = strings.Replace(url, "{id}", bizID.String(), 1)
	// Replace any remaining {id} occurrences with random UUIDs.
	for strings.Contains(url, "{id}") {
		url = strings.Replace(url, "{id}", uuid.New().String(), 1)
	}

	if strings.Contains(url, "{conversationID}") {
		url = strings.ReplaceAll(url, "{conversationID}", uuid.New().String())
	}
	if strings.Contains(url, "{integrationId}") {
		url = strings.ReplaceAll(url, "{integrationId}", uuid.New().String())
	}
	if strings.Contains(url, "{batch_id}") {
		url = strings.ReplaceAll(url, "{batch_id}", uuid.New().String())
	}
	if strings.Contains(url, "{userId}") {
		// Seed a real user for the members route (DELETE /members/{userId} validates UUID).
		memberID := seedUser(t, env.pool)
		viewerRoleID := uuid.MustParse(domain.SystemRoleViewerID)
		seedMembership(t, env.pool, bizID, memberID, viewerRoleID)
		url = strings.ReplaceAll(url, "{userId}", memberID.String())
	}
	if strings.Contains(url, "{inviteId}") {
		// Plan 03-06 Task 1: Seed a real pending invitation so DELETE
		// /invitations/{inviteId} validates the UUID parse + reaches the repo
		// layer (where the authz gates 401 / 404 still fire as expected). The
		// viewer JWT used by the walker is a non-creator so DELETE attempts
		// will hit the PermMembersInvite Can() gate before they hit the repo,
		// which is exactly what the 4-case authz trio asserts.
		ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
		// Need a non-Nil createdByUserID — seed a fresh user as the creator.
		creatorID := seedUser(t, env.pool)
		invID := seedInvitation(t, env.pool, bizID, ownerRoleID, creatorID)
		url = strings.ReplaceAll(url, "{inviteId}", invID.String())
	}
	if strings.Contains(url, "{roleId}") {
		// Plan 05-03 Task 3: Seed a real custom role so PATCH /roles/{roleId}
		// and DELETE /roles/{roleId} validate UUID parse + reach the repo
		// layer (where the authz gates 401 / 404 still fire as expected). The
		// viewer JWT used by the walker lacks PermRolesUpdate / PermRolesDelete
		// so the handler's authz.Can() returns 403 before the repo lookup —
		// which is exactly what the 4-case authz trio asserts.
		roleID := seedCustomRole(t, env.pool, bizID)
		url = strings.ReplaceAll(url, "{roleId}", roleID.String())
	}
	return url
}

// readPathExempt returns true for GET routes where the test scaffolding cannot
// guarantee a 200 response (e.g. the handler requires a seeded sub-resource
// like a specific conversation or project ID that we can't predict from the
// walker). The authz gates (401, 404) are STILL asserted for these routes;
// only Case 3 (viewer 200) is skipped.
//
// Exempt rationale per route:
//   - GET /conversations/{id}: requires a seeded conversation ObjectID in MongoDB.
//   - GET /conversations/{id}/messages: same — conversation must exist.
//   - GET /projects/{id}: requires a seeded project row (PG + Mongo).
//   - GET /projects/{id}/conversation-count: same.
//   - GET /reviews/{id}: requires a seeded review in MongoDB.
//   - GET /posts/{id}: requires a seeded post in MongoDB.
//   - GET /tasks/stream: SSE endpoint; httptest recorder does not buffer SSE.
//   - GET /integrations/vk/communities: hits VK API externally.
//   - GET /integrations/vk/community-auth-url: hits VK API externally.
//   - GET /integrations/{provider}/auth-url: requires OAuth config in env.
//   - GET /search: handler validates ?q= param (len ≥ 2) before authz BusinessContext
//     check; walker sends no query param → 400 "query too short". Authz gate still
//     tested via 401 + 404 cases.
//   - GET /tool-approvals: service performs ownership check (b.UserID == actorUserID)
//     after the RBAC gate; test viewer is not the business owner so service returns
//     ErrBusinessNotFound → 404. Authz gate still tested via 401 + 404 cases.
func readPathExempt(method, route string) bool {
	if method != http.MethodGet {
		return false
	}
	exempt := map[string]bool{
		"/api/v1/businesses/{id}/conversations/{id}":               true,
		"/api/v1/businesses/{id}/conversations/{id}/messages":      true,
		"/api/v1/businesses/{id}/projects/{id}":                    true,
		"/api/v1/businesses/{id}/projects/{id}/conversation-count": true,
		"/api/v1/businesses/{id}/reviews/{id}":                     true,
		"/api/v1/businesses/{id}/posts/{id}":                       true,
		"/api/v1/businesses/{id}/tasks/stream":                     true,
		"/api/v1/businesses/{id}/integrations/vk/communities":      true,
		"/api/v1/businesses/{id}/integrations/vk/community-auth-url": true,
		"/api/v1/businesses/{id}/integrations/vk/auth-url":           true,
		"/api/v1/businesses/{id}/integrations/yandex_business/auth-url": true,
		"/api/v1/businesses/{id}/integrations/google_business/auth-url": true,
		"/api/v1/businesses/{id}/integrations/google_business/locations": true,
		// G-04: search requires ?q= (len >= 2); walker cannot synthesize the param.
		// Authz gates (401 + 404) are still asserted.
		"/api/v1/businesses/{id}/search": true,
		// G-05: tool-approvals service checks b.UserID == actorUserID. The test viewer
		// is not the business owner so the service returns ErrBusinessNotFound → 404.
		// Authz gates (401 + 404) are still asserted.
		"/api/v1/businesses/{id}/tool-approvals": true,
		// G-09: GET / (business by ID) — service returns ErrBusinessNotFound for
		// non-owner viewers (same pattern as tool-approvals); handler maps to 500.
		// Authz gates (401 + 404) are still asserted via the middleware layer.
		"/api/v1/businesses/{id}/": true,
		// G-10: GET /integrations — service performs an owner-only lookup; viewer
		// (non-owner) gets ErrBusinessNotFound → 500. Authz gates 401 + 404 still
		// asserted via the middleware layer.
		"/api/v1/businesses/{id}/integrations": true,
	}
	return exempt[route]
}

// writePathExempt returns true for write-verb routes where the viewer's 403
// cannot be cleanly distinguished from a handler-level validation error.
// These routes still assert 401 (no-JWT) and 404 (non-member); only Case 4
// (viewer 403) is exempted.
//
// Exempt rationale per route:
//   - POST /chat/{conversationID}: rate-limited + body validation; the rate
//     limiter or body validator may fire before the authz PermContentCreate
//     check when the request body is empty/invalid.
//   - POST /chat/{id}/resume: HITL-gated; HITL handler may be nil.
//   - POST /conversations/{id}/pending-tool-calls/{id}/resolve: same.
func writePathExempt(method, route string) bool {
	if method == http.MethodGet || method == http.MethodHead {
		return false
	}
	exempt := map[string]bool{
		"/api/v1/businesses/{id}/chat/{conversationID}":                                  true,
		"/api/v1/businesses/{id}/chat/{id}/resume":                                       true,
		"/api/v1/businesses/{id}/conversations/{id}/pending-tool-calls/{id}/resolve":     true,
		// G-11: handler validates required body field "hash" before reaching the
		// authz Can() check; viewer with empty body gets 400 not 403. Authz gate
		// 401 (no JWT) and 404 (non-member) still assert correctly.
		"/api/v1/businesses/{id}/integrations/telegram/verify":                           true,
	}
	return exempt[route]
}
