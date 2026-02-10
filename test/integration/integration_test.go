package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationManagement(t *testing.T) {
	cleanupDatabase(t)

	// Setup: create user and business
	accessToken := setupTestUser(t, "integration@example.com", "password123")
	setupTestBusiness(t, accessToken)

	t.Run("ListIntegrationsEmpty", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/integrations", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result []interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Empty(t, result)
	})

	t.Run("ConnectIntegrationNotImplemented", func(t *testing.T) {
		payload := map[string]interface{}{
			"code": "test-auth-code",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", baseURL+"/api/v1/integrations/google_business/connect", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should return 501 Not Implemented
		assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
	})

	t.Run("DeleteNonExistentIntegration", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", baseURL+"/api/v1/integrations/google_business", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should return 404 Not Found
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("ConnectInvalidPlatform", func(t *testing.T) {
		payload := map[string]interface{}{
			"code": "test-auth-code",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", baseURL+"/api/v1/integrations/invalid_platform/connect", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should return 501 Not Implemented (or could be 400 Bad Request)
		assert.True(t, resp.StatusCode == http.StatusNotImplemented || resp.StatusCode == http.StatusBadRequest)
	})
}

// Helper function to create a test business
func setupTestBusiness(t *testing.T, accessToken string) {
	payload := map[string]interface{}{
		"name":        "Test Business",
		"category":    "coffee_shop",
		"address":     "123 Test St",
		"phone":       "+1234567890",
		"description": "Test business",
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PUT", baseURL+"/api/v1/business", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
}
