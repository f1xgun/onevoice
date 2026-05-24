package tokenclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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
		require.NoError(t, json.NewEncoder(w).Encode(want))
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
		require.NoError(t, json.NewEncoder(w).Encode(&TokenResponse{AccessToken: "tok"}))
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
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIntegrationNotFound),
		"404 must surface as ErrIntegrationNotFound; got %v", err)
	// Wire-format invariant: log greps depend on this exact prefix.
	assert.Contains(t, err.Error(), "tokenclient: integration not found")
}

func TestGetToken_Gone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	client := New(srv.URL, nil)
	_, err := client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTokenExpired),
		"410 must surface as ErrTokenExpired; got %v", err)
	assert.Contains(t, err.Error(), "tokenclient: token expired and refresh failed")
}

// TestGetToken_ServerError_IsTransient covers the 5xx bucket: a flaky
// upstream surfaces as ErrTransient so callers can mark it retryable
// instead of blanket-permanent.
func TestGetToken_ServerError_IsTransient(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			client := New(srv.URL, nil)
			_, err := client.GetToken(context.Background(), "b", "vk", "g")
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrTransient),
				"%d must surface as ErrTransient; got %v", status, err)
		})
	}
}

// TestGetToken_NetworkError_IsTransient covers the network-failure leg:
// connection refused / DNS failure / TLS hiccup chains ErrTransient and
// preserves the underlying error for diagnostics.
func TestGetToken_NetworkError_IsTransient(t *testing.T) {
	// Listen on a port, then close it — the next Dial gets connection refused.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	client := New("http://"+addr, &http.Client{Timeout: 500 * time.Millisecond})
	_, err = client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTransient),
		"network failure must surface as ErrTransient; got %v", err)
}

// TestGetToken_UnexpectedNon5xx_NoSentinel covers 4xx-other-than-404/410:
// likely a request-shape bug rather than a transient outage. No sentinel
// is chained so the caller's default NonRetryable classification holds.
func TestGetToken_UnexpectedNon5xx_NoSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := New(srv.URL, nil)
	_, err := client.GetToken(context.Background(), "b", "vk", "g")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrTransient),
		"4xx other than 404/410 must NOT chain ErrTransient — likely a bug not a blip")
	assert.False(t, errors.Is(err, ErrIntegrationNotFound))
	assert.False(t, errors.Is(err, ErrTokenExpired))
}

func TestGetToken_CacheEvictsExpiringSoon(t *testing.T) {
	var callCount atomic.Int32
	expiresAt := time.Now().Add(30 * time.Second) // expires in 30s (< 5 min threshold)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		require.NoError(t, json.NewEncoder(w).Encode(&TokenResponse{
			AccessToken: "tok",
			ExpiresAt:   &expiresAt,
		}))
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
