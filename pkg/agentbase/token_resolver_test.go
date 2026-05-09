package agentbase_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
)

// TestNewTokenResolver_NilClient_Panics asserts the boot-time fail-fast contract.
// Wiring bugs (passing a nil *tokenclient.Client) must surface at NewTokenResolver
// time, not at the first GetToken call. This mirrors the chat_proxy.go panic-on-nil
// pattern.
func TestNewTokenResolver_NilClient_Panics(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "NewTokenResolver(nil) must panic")
		msg, ok := r.(string)
		require.True(t, ok, "panic value should be a string, got %T", r)
		assert.Contains(t, msg, "agentbase.NewTokenResolver")
		assert.Contains(t, msg, "client cannot be nil")
	}()

	_ = agentbase.NewTokenResolver(nil)
}

// TestTokenResolver_GetToken_DelegatesToClient verifies that:
//  1. businessID / platform / externalID flow through to the upstream HTTP request
//     unchanged (no extra encoding, no field swap)
//  2. AccessToken / UserToken / ExternalID round-trip from TokenResponse to TokenInfo
//
// UserToken specifically must propagate — the VK agent depends on it (see
// services/agent-vk/internal/agent/handler.go:201-202). If this test ever passes
// without asserting UserToken, the four-agent migration will silently break VK
// private-data reads.
func TestTokenResolver_GetToken_DelegatesToClient(t *testing.T) {
	want := &tokenclient.TokenResponse{
		IntegrationID: "int-vk-1",
		Platform:      "vk",
		ExternalID:    "group-456",
		AccessToken:   "community-token-secret",
		UserToken:     "user-token-secret",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/v1/tokens", r.URL.Path)
		assert.Equal(t, "biz-1", r.URL.Query().Get("business_id"))
		assert.Equal(t, "vk", r.URL.Query().Get("platform"))
		assert.Equal(t, "group-456", r.URL.Query().Get("external_id"))
		require.NoError(t, json.NewEncoder(w).Encode(want))
	}))
	defer srv.Close()

	resolver := agentbase.NewTokenResolver(tokenclient.New(srv.URL, nil))
	got, err := resolver.GetToken(context.Background(), "biz-1", "vk", "group-456")
	require.NoError(t, err)

	assert.Equal(t, "community-token-secret", got.AccessToken)
	assert.Equal(t, "user-token-secret", got.UserToken,
		"UserToken must propagate — VK agent depends on it (see services/agent-vk/internal/agent/handler.go:201)")
	assert.Equal(t, "group-456", got.ExternalID)
}

// TestTokenResolver_GetToken_EmptyExternalID_Propagates verifies the
// fallback-on-empty contract. tokenclient.Client treats empty externalID as a
// signal to fall back to the first active integration; the resolver MUST forward
// the empty string verbatim — not coerce to "0", "default", or similar.
func TestTokenResolver_GetToken_EmptyExternalID_Propagates(t *testing.T) {
	var receivedExternalID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedExternalID = r.URL.Query().Get("external_id")
		require.NoError(t, json.NewEncoder(w).Encode(&tokenclient.TokenResponse{
			AccessToken: "fallback-token",
			ExternalID:  "resolved-by-server",
		}))
	}))
	defer srv.Close()

	resolver := agentbase.NewTokenResolver(tokenclient.New(srv.URL, nil))
	got, err := resolver.GetToken(context.Background(), "biz-1", "telegram", "")
	require.NoError(t, err)

	assert.Equal(t, "", receivedExternalID, "empty externalID must reach upstream as empty")
	assert.Equal(t, "fallback-token", got.AccessToken)
	// The server's resolved externalID flows back to the caller — same semantic
	// the API's GetDecryptedToken honors.
	assert.Equal(t, "resolved-by-server", got.ExternalID)
}

// TestTokenResolver_GetToken_NoUserToken_LeavesEmpty verifies that platforms
// without a separate user-scoped token (telegram / yandex / google) get an
// empty UserToken in the returned TokenInfo. Consumers treat empty as absent.
func TestTokenResolver_GetToken_NoUserToken_LeavesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Telegram-style response — UserToken omitted (json:"omitempty").
		require.NoError(t, json.NewEncoder(w).Encode(&tokenclient.TokenResponse{
			AccessToken: "bot-token",
			ExternalID:  "channel-1001",
		}))
	}))
	defer srv.Close()

	resolver := agentbase.NewTokenResolver(tokenclient.New(srv.URL, nil))
	got, err := resolver.GetToken(context.Background(), "biz-1", "telegram", "channel-1001")
	require.NoError(t, err)

	assert.Equal(t, "bot-token", got.AccessToken)
	assert.Equal(t, "", got.UserToken, "non-VK platforms must return empty UserToken")
	assert.Equal(t, "channel-1001", got.ExternalID)
}

// TestTokenResolver_GetToken_ErrorPropagates verifies that errors from the
// underlying *tokenclient.Client are returned without modification, with an
// empty TokenInfo. Callers (the agent handlers) wrap the error themselves with
// a2a.NewNonRetryableError when appropriate.
func TestTokenResolver_GetToken_ErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	resolver := agentbase.NewTokenResolver(tokenclient.New(srv.URL, nil))
	got, err := resolver.GetToken(context.Background(), "biz-1", "vk", "group-456")
	require.Error(t, err)
	assert.Equal(t, agentbase.TokenInfo{}, got, "error path must return zero TokenInfo")
	assert.Contains(t, err.Error(), "not found")
}
