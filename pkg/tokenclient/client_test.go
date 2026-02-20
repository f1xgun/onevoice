package tokenclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetToken_FetchesFromAPI(t *testing.T) {
	want := &TokenResponse{
		IntegrationID: "int-123",
		Platform:      "vk",
		ExternalID:    "group-456",
		AccessToken:   "secret-token",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/v1/tokens", r.URL.Path)
		assert.Equal(t, "biz-1", r.URL.Query().Get("business_id"))
		assert.Equal(t, "vk", r.URL.Query().Get("platform"))
		assert.Equal(t, "group-456", r.URL.Query().Get("external_id"))
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	client := New(srv.URL, nil)
	got, err := client.GetToken(context.Background(), "biz-1", "vk", "group-456")
	require.NoError(t, err)
	assert.Equal(t, "secret-token", got.AccessToken)
	assert.Equal(t, "group-456", got.ExternalID)
}

func TestGetToken_CachesResult(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(&TokenResponse{AccessToken: "tok"})
	}))
	defer srv.Close()

	client := New(srv.URL, nil)

	_, err := client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)

	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)

	assert.Equal(t, int32(1), callCount.Load(), "should only call API once due to caching")
}

func TestGetToken_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(srv.URL, nil)
	_, err := client.GetToken(context.Background(), "b", "vk", "g")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetToken_Gone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	client := New(srv.URL, nil)
	_, err := client.GetToken(context.Background(), "b", "vk", "g")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestGetToken_CacheEvictsExpiringSoon(t *testing.T) {
	var callCount atomic.Int32
	expiresAt := time.Now().Add(30 * time.Second) // expires in 30s (< 1 min threshold)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(&TokenResponse{
			AccessToken: "tok",
			ExpiresAt:   &expiresAt,
		})
	}))
	defer srv.Close()

	client := New(srv.URL, nil)

	_, err := client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)

	// Second call should fetch again because token expires within 1 minute
	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.NoError(t, err)

	assert.Equal(t, int32(2), callCount.Load(), "should call API twice since token is expiring soon")
}
