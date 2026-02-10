package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthFlow(t *testing.T) {
	cleanupDatabase(t)

	var accessToken, refreshToken string
	var userID string

	t.Run("Register", func(t *testing.T) {
		payload := map[string]string{
			"email":    "test@example.com",
			"password": "password123",
		}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.NotEmpty(t, result["id"])
		assert.Equal(t, "test@example.com", result["email"])
		userID = result["id"].(string)
	})

	t.Run("RegisterDuplicate", func(t *testing.T) {
		payload := map[string]string{
			"email":    "test@example.com",
			"password": "password123",
		}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusConflict, resp.StatusCode)
	})

	t.Run("Login", func(t *testing.T) {
		payload := map[string]string{
			"email":    "test@example.com",
			"password": "password123",
		}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.NotEmpty(t, result["accessToken"])
		assert.NotEmpty(t, result["refreshToken"])

		accessToken = result["accessToken"].(string)
		refreshToken = result["refreshToken"].(string)
	})

	t.Run("LoginInvalidCredentials", func(t *testing.T) {
		payload := map[string]string{
			"email":    "test@example.com",
			"password": "wrongpassword",
		}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Me", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Equal(t, userID, result["id"])
		assert.Equal(t, "test@example.com", result["email"])
	})

	t.Run("MeWithoutAuth", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/auth/me", nil)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("RefreshToken", func(t *testing.T) {
		payload := map[string]string{"refreshToken": refreshToken}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/refresh", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.NotEmpty(t, result["accessToken"])

		// Update access token
		accessToken = result["accessToken"].(string)
	})

	t.Run("RefreshTokenInvalid", func(t *testing.T) {
		payload := map[string]string{"refreshToken": "invalid-token"}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/refresh", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Logout", func(t *testing.T) {
		payload := map[string]string{"refreshToken": refreshToken}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", baseURL+"/api/v1/auth/logout", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	t.Run("RefreshAfterLogout", func(t *testing.T) {
		payload := map[string]string{"refreshToken": refreshToken}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/refresh", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should fail because token is invalidated
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}
