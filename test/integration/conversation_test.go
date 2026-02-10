package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationManagement(t *testing.T) {
	cleanupDatabase(t)

	// Setup: create user
	accessToken := setupTestUser(t, "conversation@example.com", "password123")

	var conversationID string

	t.Run("ListConversationsEmpty", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result []interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Empty(t, result)
	})

	t.Run("CreateConversation", func(t *testing.T) {
		payload := map[string]interface{}{
			"title": "Test Conversation",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", baseURL+"/api/v1/conversations", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.NotEmpty(t, result["id"])
		assert.Equal(t, "Test Conversation", result["title"])
		assert.NotEmpty(t, result["userId"])

		conversationID = result["id"].(string)
	})

	t.Run("ListConversations", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Len(t, result, 1)
		assert.Equal(t, conversationID, result[0]["id"])
		assert.Equal(t, "Test Conversation", result[0]["title"])
	})

	t.Run("GetConversation", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations/"+conversationID, nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Equal(t, conversationID, result["id"])
		assert.Equal(t, "Test Conversation", result["title"])
	})

	t.Run("GetNonExistentConversation", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations/507f1f77bcf86cd799439011", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("GetConversationInvalidID", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations/invalid-id", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("CreateMultipleConversations", func(t *testing.T) {
		// Create 3 more conversations
		for i := 1; i <= 3; i++ {
			payload := map[string]interface{}{
				"title": fmt.Sprintf("Conversation %d", i),
			}
			body, _ := json.Marshal(payload)

			req, _ := http.NewRequest("POST", baseURL+"/api/v1/conversations", bytes.NewBuffer(body))
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("Content-Type", "application/json")

			resp, err := httpClient.Do(req)
			require.NoError(t, err)
			resp.Body.Close()
			assert.Equal(t, http.StatusCreated, resp.StatusCode)
		}

		// List all conversations
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/conversations", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		var result []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Len(t, result, 4) // 1 initial + 3 new
	})
}
