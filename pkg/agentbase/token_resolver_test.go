package agentbase_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
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

	tc, err := tokenclient.New(srv.URL, nil)
	require.NoError(t, err)
	resolver := agentbase.NewTokenResolver(tc)
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

	tc, err := tokenclient.New(srv.URL, nil)
	require.NoError(t, err)
	resolver := agentbase.NewTokenResolver(tc)
	got, err := resolver.GetToken(context.Background(), "biz-1", "telegram", "")
	require.NoError(t, err)

	assert.Equal(t, "", receivedExternalID, "empty externalID must reach upstream as empty")
	assert.Equal(t, "fallback-token", got.AccessToken)
	assert.Equal(t, "resolved-by-server", got.ExternalID)
}

// TestTokenResolver_GetToken_NoUserToken_LeavesEmpty verifies that platforms
// without a separate user-scoped token (telegram / yandex / google) get an
// empty UserToken in the returned TokenInfo. Consumers treat empty as absent.
func TestTokenResolver_GetToken_NoUserToken_LeavesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(&tokenclient.TokenResponse{
			AccessToken: "bot-token",
			ExternalID:  "channel-1001",
		}))
	}))
	defer srv.Close()

	tc, err := tokenclient.New(srv.URL, nil)
	require.NoError(t, err)
	resolver := agentbase.NewTokenResolver(tc)
	got, err := resolver.GetToken(context.Background(), "biz-1", "telegram", "channel-1001")
	require.NoError(t, err)

	assert.Equal(t, "bot-token", got.AccessToken)
	assert.Equal(t, "", got.UserToken, "non-VK platforms must return empty UserToken")
	assert.Equal(t, "channel-1001", got.ExternalID)
}

// TestTokenResolver_GetToken_ErrorPropagates verifies that errors from the
// underlying *tokenclient.Client are returned without modification, with an
// empty TokenInfo. The sentinel chain (tokenclient.ErrIntegrationNotFound)
// must survive the resolver so callers can use errors.Is upstream.
func TestTokenResolver_GetToken_ErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tc, err := tokenclient.New(srv.URL, nil)
	require.NoError(t, err)
	resolver := agentbase.NewTokenResolver(tc)
	got, err := resolver.GetToken(context.Background(), "biz-1", "vk", "group-456")
	require.Error(t, err)
	assert.Equal(t, agentbase.TokenInfo{}, got, "error path must return zero TokenInfo")
	assert.True(t, errors.Is(err, tokenclient.ErrIntegrationNotFound),
		"sentinel chain must survive the resolver; got %v", err)
}

// TestWrapTokenFetchError covers the canonical retryability policy shared
// by all four agent handlers. ErrTransient stays a bare error (retryable);
// every other classification wraps as *a2a.NonRetryableError.
func TestWrapTokenFetchError(t *testing.T) {
	t.Run("nil_passthrough", func(t *testing.T) {
		assert.Nil(t, agentbase.WrapTokenFetchError(nil))
	})

	t.Run("transient_stays_retryable", func(t *testing.T) {
		ctx := fmt.Errorf("fetch token: %w", tokenclient.ErrTransient)
		out := agentbase.WrapTokenFetchError(ctx)
		require.Error(t, out)
		assert.False(t, errors.Is(out, &a2a.NonRetryableError{}),
			"ErrTransient must NOT be wrapped as NonRetryable; callers can mark transient and retry")
		assert.True(t, errors.Is(out, tokenclient.ErrTransient),
			"sentinel chain must survive WrapTokenFetchError")
	})

	t.Run("not_found_marks_non_retryable", func(t *testing.T) {
		ctx := fmt.Errorf("fetch token: %w", tokenclient.ErrIntegrationNotFound)
		out := agentbase.WrapTokenFetchError(ctx)
		require.Error(t, out)
		assert.True(t, errors.Is(out, &a2a.NonRetryableError{}),
			"ErrIntegrationNotFound is permanent until the user reconnects — must mark NonRetryable")
		assert.True(t, errors.Is(out, tokenclient.ErrIntegrationNotFound),
			"sentinel chain must survive the wrap")
	})

	t.Run("token_expired_marks_non_retryable", func(t *testing.T) {
		ctx := fmt.Errorf("fetch token: %w", tokenclient.ErrTokenExpired)
		out := agentbase.WrapTokenFetchError(ctx)
		require.Error(t, out)
		assert.True(t, errors.Is(out, &a2a.NonRetryableError{}),
			"ErrTokenExpired is permanent until re-auth — must mark NonRetryable")
	})

	t.Run("unclassified_marks_non_retryable", func(t *testing.T) {
		out := agentbase.WrapTokenFetchError(errors.New("tokenclient: unexpected status 400"))
		require.Error(t, out)
		assert.True(t, errors.Is(out, &a2a.NonRetryableError{}),
			"unclassified errors must default to NonRetryable (do-not-retry posture is safer)")
	})
}
