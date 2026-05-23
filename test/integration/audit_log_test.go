package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestAuditLogEndpoint_E2E exercises GET /api/v1/businesses/{id}/audit-logs
// end-to-end against the live API + Postgres: it seeds 5 audit rows
// (covering all 5 categories + one NULL-user failed-login), then hits the
// endpoint as owner / viewer / unauthenticated to verify the full
// auth + RBAC + cursor + filter contract that Plan 19-06 frontend consumes.
//
// Why insert audit rows directly via SQL (not via the live audit writer):
// Wave 3's writer is async fire-and-forget. Triggering rows via real
// mutations would race against the test's read; the direct INSERT path
// also lets us pin created_at deterministically so cursor ordering is
// reproducible.
//
// Why seed the Viewer membership via raw SQL: the create-invitation +
// accept-invitation flow requires the email-link side channel which the
// integration harness doesn't simulate. Direct INSERT into business_members
// with role_id = SystemRoleViewerID is the same end state — and the
// RequireBusinessAccess middleware reads from business_members directly.
func TestAuditLogEndpoint_E2E(t *testing.T) {
	if pgPool == nil || baseURL == "" {
		t.Skip("integration env not set; skipping")
	}
	ctx := context.Background()

	// --- Set up Owner + Business + Viewer ---

	ownerEmail := "audit-owner-" + uuid.NewString() + "@test.local"
	ownerToken := setupTestUser(t, ownerEmail, "SecretPass123")

	// Resolve the owner's user_id via /auth/me so we can attach it to
	// seed audit rows. /auth/me is the only canonical "who am I" surface.
	ownerID := whoami(t, ownerToken)

	// Owner creates the business through the live POST endpoint —
	// this dual-writes businesses + business_members(role_id=Owner) so the
	// RequireBusinessAccess middleware sees the owner.
	businessID := createBusinessAndReturnID(t, ownerToken, "AuditTestBiz-"+uuid.NewString()[:8])

	// Viewer: register a second user, then INSERT a viewer membership for
	// the same business directly (bypassing invitation email flow).
	viewerEmail := "audit-viewer-" + uuid.NewString() + "@test.local"
	viewerToken := setupTestUser(t, viewerEmail, "SecretPass123")
	viewerID := whoami(t, viewerToken)

	viewerRoleID := uuid.MustParse(domain.SystemRoleViewerID)
	_, err := pgPool.Exec(ctx, `
		INSERT INTO business_members (business_id, user_id, role_id, status, joined_at)
		VALUES ($1, $2, $3, 'active', now())`,
		businessID, viewerID, viewerRoleID,
	)
	require.NoError(t, err, "seed viewer membership")

	// Per-test cleanup: rip out audit + members + business + users in
	// dependency order. The business-members trigger and FK cascades
	// would otherwise refuse delete.
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM audit_logs WHERE business_id = $1`, businessID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM business_members WHERE business_id = $1`, businessID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM businesses WHERE id = $1`, businessID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM users WHERE id = $1`, ownerID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM users WHERE id = $1`, viewerID)
	})

	// --- Seed 5 audit rows with explicit created_at for stable ordering ---
	//
	// Order (newest → oldest, matching ORDER BY created_at DESC):
	//   [0] integration.connected   actor=viewer  t = base
	//   [1] business.created        actor=viewer  t = base - 1m
	//   [2] rbac.role_granted       actor=viewer  t = base - 2m
	//   [3] auth.login_success      actor=viewer  t = base - 3m
	//   [4] auth.login_failed       actor=NULL    t = base - 4m
	//
	// We use INSERT ... (id, business_id, user_id, action, resource, details, created_at)
	// so created_at is deterministic; defaults would tie on the same
	// microsecond and the cursor tie-breaker on id would surface as a
	// noisy flake.
	base := time.Now().UTC().Add(-1 * time.Hour) // 1h in the past avoids "to=now" boundary surprises
	seedRows := []struct {
		action   string
		resource string
		userID   *uuid.UUID
		details  string
	}{
		{"integration.connected", "integration", &viewerID, `{"integration_id":"00000000-0000-0000-0000-000000000111","platform":"telegram","external_id":"@test"}`},
		{"business.created", "business", &viewerID, `{"name":"AuditTestBiz"}`},
		{"rbac.role_granted", "role", &viewerID, `{"target_user_id":"00000000-0000-0000-0000-000000000222","new_role_id":"00000000-0000-0000-0000-000000000333"}`},
		{"auth.login_success", "user", &viewerID, `{"ip":"1.2.3.4","user_agent":"test"}`},
		{"auth.login_failed", "user", nil, `{"attempted_email":"intruder@example.com","ip":"5.6.7.8","reason":"bad_password"}`},
	}
	for i, row := range seedRows {
		_, err := pgPool.Exec(ctx, `
			INSERT INTO audit_logs (id, business_id, user_id, action, resource, details, created_at)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)`,
			uuid.New(), businessID, row.userID, row.action, row.resource,
			row.details, base.Add(-time.Duration(i)*time.Minute),
		)
		require.NoError(t, err, "seed audit row %d", i)
	}

	// --- Subtests ---

	t.Run("list_first_page", func(t *testing.T) {
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?limit=3", businessID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var body struct {
			Items      []map[string]any `json:"items"`
			NextCursor *string          `json:"next_cursor"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Len(t, body.Items, 3)
		require.NotNil(t, body.NextCursor, "page is full ⇒ next_cursor must be present")

		// Newest first: action[0] should be "integration.connected"
		assert.Equal(t, "integration.connected", body.Items[0]["action"])
		assert.Equal(t, "integration", body.Items[0]["action_category"])
	})

	t.Run("list_second_page_via_cursor", func(t *testing.T) {
		// Fetch page 1 to get the cursor.
		resp1 := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?limit=3", businessID))
		var p1 struct {
			NextCursor *string `json:"next_cursor"`
		}
		require.NoError(t, json.NewDecoder(resp1.Body).Decode(&p1))
		resp1.Body.Close()
		require.NotNil(t, p1.NextCursor)

		// Use that cursor to fetch page 2.
		resp2 := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?limit=3&cursor=%s", businessID, *p1.NextCursor))
		defer resp2.Body.Close()
		require.Equal(t, http.StatusOK, resp2.StatusCode)
		var p2 struct {
			Items      []map[string]any `json:"items"`
			NextCursor *string          `json:"next_cursor"`
		}
		require.NoError(t, json.NewDecoder(resp2.Body).Decode(&p2))
		require.Len(t, p2.Items, 2, "5 total - 3 first page = 2 second page")
		require.Nil(t, p2.NextCursor, "end of stream ⇒ next_cursor null")
	})

	t.Run("filter_category", func(t *testing.T) {
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?category=auth", businessID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Len(t, body.Items, 2, "auth.* category has 2 rows (login_success + login_failed)")
		for _, item := range body.Items {
			assert.Equal(t, "auth", item["action_category"])
		}
	})

	t.Run("filter_action_specific", func(t *testing.T) {
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?action=auth.login_failed", businessID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Len(t, body.Items, 1)
		assert.Equal(t, "auth.login_failed", body.Items[0]["action"])
		// failed-login row has user_id=NULL → JSON actor_email is null
		// (LEFT JOIN found no users row → COALESCE → '' → nil pointer).
		assert.Nil(t, body.Items[0]["actor_email"], "failed-login row has null actor_email")
		assert.Nil(t, body.Items[0]["actor_id"], "failed-login row has null actor_id")
	})

	t.Run("filter_actor", func(t *testing.T) {
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?actor=%s", businessID, viewerID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		// 4 of 5 seeded rows have user_id = viewer (the failed-login row
		// has NULL user_id and must be excluded).
		require.Len(t, body.Items, 4)
		for _, item := range body.Items {
			assert.Equal(t, viewerID.String(), item["actor_id"])
		}
	})

	t.Run("actor_email_enrichment", func(t *testing.T) {
		// At least one row must have actor_email = viewerEmail
		// (LEFT JOIN users populated) — proves the single-query join works
		// against real PG.
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?actor=%s&limit=1", businessID, viewerID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body struct {
			Items []map[string]any `json:"items"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.NotEmpty(t, body.Items)
		assert.Equal(t, viewerEmail, body.Items[0]["actor_email"],
			"LEFT JOIN should populate actor_email from users table")
	})

	t.Run("viewer_forbidden", func(t *testing.T) {
		resp := authedGet(t, viewerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs", businessID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusForbidden, resp.StatusCode,
			"viewer role lacks PermAuditRead ⇒ 403")
	})

	t.Run("unauthenticated_401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+fmt.Sprintf("/api/v1/businesses/%s/audit-logs", businessID), nil)
		// No Authorization header.
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("invalid_cursor_400", func(t *testing.T) {
		// Use a base64-url-safe-looking but bad payload so the request
		// gets to DecodeCursor (a base64 decode failure also maps to
		// invalid_cursor — see TestAuditLogHandler_List_InvalidCursor_400).
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?cursor=corrupt", businessID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var body struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "invalid_cursor", body.Error)
	})

	t.Run("invalid_category_400", func(t *testing.T) {
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?category=unknown", businessID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var body struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "invalid_category", body.Error)
	})

	t.Run("invalid_limit_400", func(t *testing.T) {
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?limit=999", businessID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var body struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "invalid_limit", body.Error)
	})

	// Type-pinned cursor round-trip: ensure the pkg/audit cursor that the
	// repo emits via the handler is the same shape the client sees.
	// (Sanity check — the encode/decode helpers are unit-tested in
	// pkg/audit; this is end-to-end confirmation against real PG.)
	t.Run("cursor_decodes_to_real_tuple", func(t *testing.T) {
		resp := authedGet(t, ownerToken, fmt.Sprintf("/api/v1/businesses/%s/audit-logs?limit=3", businessID))
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body struct {
			Items      []map[string]any `json:"items"`
			NextCursor *string          `json:"next_cursor"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.NotNil(t, body.NextCursor)

		gotT, gotID, err := audit.DecodeCursor(*body.NextCursor)
		require.NoError(t, err)
		// Cursor MUST encode the LAST row of the page.
		lastItem := body.Items[len(body.Items)-1]
		assert.Equal(t, lastItem["id"], gotID.String(),
			"cursor.id MUST equal last row's id")
		lastCreatedAt, err := time.Parse(time.RFC3339Nano, lastItem["created_at"].(string))
		require.NoError(t, err)
		assert.True(t, lastCreatedAt.UTC().Equal(gotT.UTC()),
			"cursor.t MUST equal last row's created_at")
	})
}

// --- helpers ---

// whoami calls GET /api/v1/auth/me with the given access token and returns
// the authenticated user's UUID. /auth/me is the only contract surface
// for "who am I"; downstream tests use this UUID to attach FK columns.
func whoami(t *testing.T, token string) uuid.UUID {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/auth/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "/auth/me failed for token")

	var body struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	id, err := uuid.Parse(body.ID)
	require.NoError(t, err)
	return id
}

// createBusinessAndReturnID hits POST /api/v1/businesses to create a
// v2-RBAC business and returns the new ID. POST is the v2-shape that
// dual-writes businesses + business_members(role_id=Owner) so the
// authorize middleware sees the owner.
func createBusinessAndReturnID(t *testing.T, token, name string) uuid.UUID {
	t.Helper()
	payload := map[string]any{
		"name":        name,
		"category":    "test",
		"address":     "1 Test St",
		"phone":       "+10000000",
		"description": "audit endpoint integration test",
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/businesses", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.True(t, resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK,
		"POST /businesses unexpected status %d", resp.StatusCode)

	var out struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	id, err := uuid.Parse(out.ID)
	require.NoError(t, err, "POST /businesses response missing valid id")
	return id
}

// authedGet does an authenticated GET; caller must Close the body.
func authedGet(t *testing.T, token, path string) *http.Response {
	t.Helper()
	url := baseURL + path
	if strings.HasPrefix(path, "http") {
		url = path
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	return resp
}
