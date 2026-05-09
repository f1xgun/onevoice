package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestBusinessCreate_DualWriteAtomic asserts DATA-06: creating a business
// inserts both a businesses row AND a business_members(role_id=owner) row
// in a single transaction. The test exercises the live HTTP path
// (PUT /api/v1/business with no existing business → handler invokes
// service.business.Create) so the transaction wiring in the service layer
// is covered, not just the repository unit tests.
//
// The rollback path (injected error mid-tx) is covered by Plan G's pgxmock
// unit tests and not re-asserted here — it would require either a fault
// injection hook or a corrupted DB state to trigger over HTTP, both of
// which are out of scope for this integration test.
func TestBusinessCreate_DualWriteAtomic(t *testing.T) {
	if pgPool == nil || baseURL == "" {
		t.Skip("integration env not set; skipping")
	}
	ctx := context.Background()

	// 1. Register a brand-new user. Auto-login returns the access token in
	//    the {user, accessToken} response shape (LoginResponse).
	email := "biz-dualwrite-" + uuid.NewString() + "@test.local"
	regBody, _ := json.Marshal(map[string]string{"email": email, "password": "SecretPass123"})
	regResp, err := httpClient.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(regBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, regResp.StatusCode)
	var regOut struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		AccessToken string `json:"accessToken"`
	}
	require.NoError(t, json.NewDecoder(regResp.Body).Decode(&regOut))
	regResp.Body.Close()
	userID, err := uuid.Parse(regOut.User.ID)
	require.NoError(t, err)
	require.NotEmpty(t, regOut.AccessToken)

	t.Cleanup(func() {
		// The BEFORE DELETE trigger on users refuses if user is sole owner of
		// any business, so cleanup deletes business_members + businesses
		// first. Use a bounded context so cleanup never hangs even if the
		// test's own ctx was cancelled.
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM business_members WHERE user_id = $1`, userID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM businesses WHERE user_id = $1`, userID)
		_, _ = pgPool.Exec(cleanCtx, `DELETE FROM users WHERE id = $1`, userID)
	})

	// 2. Create the business via PUT /api/v1/business. With no existing
	//    business for the user, the handler invokes service.business.Create
	//    which dual-writes businesses + business_members in one tx and
	//    returns 201 (UpdateBusiness handler at services/api/internal/handler/business.go).
	bizBody, _ := json.Marshal(map[string]interface{}{
		"name":        "DualWriteCo",
		"category":    "test",
		"address":     "1 Test St",
		"phone":       "+10000000",
		"description": "phase 1 dual-write",
	})
	req, err := http.NewRequest(http.MethodPut, baseURL+"/api/v1/business", bytes.NewReader(bizBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+regOut.AccessToken)
	bizResp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer bizResp.Body.Close()
	require.Equal(t, http.StatusCreated, bizResp.StatusCode, "PUT /business should create a new business via service.Create")

	var biz struct {
		ID     string `json:"id"`
		UserID string `json:"userId"`
	}
	require.NoError(t, json.NewDecoder(bizResp.Body).Decode(&biz))
	bizID, err := uuid.Parse(biz.ID)
	require.NoError(t, err)

	// 3. Assert both rows exist atomically.
	var bizCount int
	require.NoError(t, pgPool.QueryRow(ctx, `SELECT COUNT(*) FROM businesses WHERE id = $1 AND user_id = $2`, bizID, userID).Scan(&bizCount))
	assert.Equal(t, 1, bizCount, "businesses row should exist after Create")

	var memberRoleID uuid.UUID
	err = pgPool.QueryRow(ctx, `SELECT role_id FROM business_members WHERE business_id = $1 AND user_id = $2`, bizID, userID).Scan(&memberRoleID)
	require.NoError(t, err, "business_members row should exist after Create (DATA-06 dual-write)")
	assert.Equal(t, uuid.MustParse(domain.SystemRoleOwnerID), memberRoleID, "owner membership role_id must equal SystemRoleOwnerID")
}
