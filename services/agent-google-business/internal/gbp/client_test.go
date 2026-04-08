package gbp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/v1/accounts", r.URL.Path)
		resp := ListAccountsResponse{
			Accounts: []Account{
				{Name: "accounts/123", AccountName: "Test Business", Type: "PERSONAL"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New("test-token")
	c.AccountsBaseURL = srv.URL

	accounts, err := c.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, "accounts/123", accounts[0].Name)
	assert.Equal(t, "Test Business", accounts[0].AccountName)
}

func TestClient_ListLocations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.URL.Path, "/v1/accounts/123/locations")
		assert.Equal(t, "name,title,storefrontAddress", r.URL.Query().Get("readMask"))
		resp := ListLocationsResponse{
			Locations: []Location{
				{Name: "locations/456", Title: "My Shop"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New("test-token")
	c.BusinessInfoBaseURL = srv.URL

	locations, err := c.ListLocations(context.Background(), "accounts/123")
	require.NoError(t, err)
	require.Len(t, locations, 1)
	assert.Equal(t, "locations/456", locations[0].Name)
	assert.Equal(t, "My Shop", locations[0].Title)
}

func TestClient_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			}{Code: 403, Message: "API not enabled", Status: "PERMISSION_DENIED"},
		})
	}))
	defer srv.Close()

	c := New("test-token")
	c.AccountsBaseURL = srv.URL

	_, err := c.ListAccounts(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "google api error")
	assert.Contains(t, err.Error(), "403")
}
