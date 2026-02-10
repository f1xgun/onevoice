package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBusinessCRUD(t *testing.T) {
	cleanupDatabase(t)

	// Setup: create user and login
	accessToken := setupTestUser(t, "business@example.com", "password123")

	var businessID string

	t.Run("GetBusinessBeforeCreation", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/business", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should return 404 since no business exists yet
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("UpdateBusinessCreatesIfNotExists", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":        "Test Coffee Shop",
			"category":    "coffee_shop",
			"address":     "123 Main St",
			"phone":       "+1234567890",
			"description": "Best coffee in town",
			"logoUrl":     "https://example.com/logo.png",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("PUT", baseURL+"/api/v1/business", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.NotEmpty(t, result["id"])
		assert.Equal(t, "Test Coffee Shop", result["name"])
		assert.Equal(t, "coffee_shop", result["category"])
		assert.Equal(t, "123 Main St", result["address"])
		assert.Equal(t, "+1234567890", result["phone"])
		assert.Equal(t, "Best coffee in town", result["description"])
		assert.Equal(t, "https://example.com/logo.png", result["logoUrl"])

		businessID = result["id"].(string)
	})

	t.Run("GetBusiness", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/business", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Equal(t, businessID, result["id"])
		assert.Equal(t, "Test Coffee Shop", result["name"])
	})

	t.Run("UpdateBusiness", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":        "Updated Coffee Shop",
			"category":    "cafe",
			"address":     "456 Oak St",
			"phone":       "+9876543210",
			"description": "Even better coffee",
			"logoUrl":     "https://example.com/new-logo.png",
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("PUT", baseURL+"/api/v1/business", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Equal(t, businessID, result["id"])
		assert.Equal(t, "Updated Coffee Shop", result["name"])
		assert.Equal(t, "cafe", result["category"])
		assert.Equal(t, "456 Oak St", result["address"])
	})

	t.Run("GetBusinessAfterUpdate", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/business", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Equal(t, "Updated Coffee Shop", result["name"])
		assert.Equal(t, "cafe", result["category"])
	})
}

// Helper function to create a test user and return access token
func setupTestUser(t *testing.T, email, password string) string {
	// Register
	payload := map[string]string{
		"email":    email,
		"password": password,
	}
	body, _ := json.Marshal(payload)

	resp, err := httpClient.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	resp.Body.Close()

	// Login
	resp, err = httpClient.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	return result["accessToken"].(string)
}
