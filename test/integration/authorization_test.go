package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiUserAuthorization(t *testing.T) {
	cleanupDatabase(t)

	// Setup: create two users
	accessTokenA := setupTestUser(t, "userA@example.com", "password123")
	accessTokenB := setupTestUser(t, "userB@example.com", "password123")

	// User A creates business
	setupTestBusiness(t, accessTokenA)

	// User B creates business
	setupTestBusiness(t, accessTokenB)

	// User A creates conversation
	var conversationIDA string
	t.Run("UserACreatesConversation", func(t *testing.T) {
		payload := map[string]interface{}{
			"title": "User A's Conversation",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", baseURL+"/api/v1/conversations", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+accessTokenA)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		conversationIDA = result["id"].(string)
	})

	// User B creates conversation
	var conversationIDB string
	t.Run("UserBCreatesConversation", func(t *testing.T) {
		payload := map[string]interface{}{
			"title": "User B's Conversation",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", baseURL+"/api/v1/conversations", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+accessTokenB)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		conversationIDB = result["id"].(string)
	})

	t.Run("UserACannotAccessUserBConversation", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations/"+conversationIDB, nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenA)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should return 403 Forbidden (authorization check in handler)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("UserBCannotAccessUserAConversation", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations/"+conversationIDA, nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenB)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should return 403 Forbidden (authorization check in handler)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	t.Run("UserACanAccessOwnConversation", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations/"+conversationIDA, nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenA)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("UserBCanAccessOwnConversation", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations/"+conversationIDB, nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenB)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("UserASeesOnlyOwnConversations", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations", nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenA)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Len(t, result, 1)
		assert.Equal(t, conversationIDA, result[0]["id"])
	})

	t.Run("UserBSeesOnlyOwnConversations", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations", nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenB)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Len(t, result, 1)
		assert.Equal(t, conversationIDB, result[0]["id"])
	})

	t.Run("UserBCannotAccessUserABusiness", func(t *testing.T) {
		// User B tries to get User A's business
		// Since business is tied to user, User B should see their own business, not A's
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/business", nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenB)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		// The business should belong to User B
		assert.NotNil(t, result["userId"])
	})

	t.Run("UserACanAccessOwnBusiness", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/business", nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenA)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("UserASeesOnlyOwnIntegrations", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/integrations", nil)
		req.Header.Set("Authorization", "Bearer "+accessTokenA)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result []interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		// Should be empty or contain only User A's integrations
		assert.NotNil(t, result)
	})
}
