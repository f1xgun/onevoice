package agentbase_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// stubResolver is a hand-rolled TokenResolver that records its last call and
// returns a canned TokenInfo/error. Used for the shared-session paths that
// don't need a full HTTP round-trip.
type stubResolver struct {
	info      agentbase.TokenInfo
	err       error
	gotBiz    string
	gotPlat   string
	gotExt    string
	gotReason string
	callCount int
}

func (s *stubResolver) GetToken(_ context.Context, businessID, platform, externalID, reason string) (agentbase.TokenInfo, error) {
	s.callCount++
	s.gotBiz, s.gotPlat, s.gotExt, s.gotReason = businessID, platform, externalID, reason
	return s.info, s.err
}

// TestNewSharedSessionResolver_NilResolver_Panics asserts the boot-time
// fail-fast: a nil TokenResolver is a wiring bug that must surface at
// construction, not at the first GetSharedSession.
func TestNewSharedSessionResolver_NilResolver_Panics(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "NewSharedSessionResolver(nil, ...) must panic")
	}()
	_ = agentbase.NewSharedSessionResolver(nil, "shared-biz")
}

// TestGetSharedSession_UnsetSentinel_FailsClosed is the fail-closed invariant:
// when YANDEX_SHARED_BUSINESS_ID is unset (empty sentinel), the delegated plane
// is inert and GetSharedSession returns ErrSharedSessionNotConfigured WITHOUT
// consulting the resolver. Reverting the empty-sentinel guard makes this fail.
func TestGetSharedSession_UnsetSentinel_FailsClosed(t *testing.T) {
	stub := &stubResolver{info: agentbase.TokenInfo{AccessToken: "should-not-be-read"}}
	r := agentbase.NewSharedSessionResolver(stub, "")

	_, err := r.GetSharedSession(context.Background(), a2a.AgentYandexBusiness, "verify")
	require.Error(t, err)
	assert.ErrorIs(t, err, agentbase.ErrSharedSessionNotConfigured)
	assert.Equal(t, 0, stub.callCount, "resolver must NOT be consulted when sentinel is unset")
}

// TestGetSharedSession_PinsSentinelCoordinates verifies the resolver is called
// with the config sentinel business, the requested platform, and the reserved
// __shared_rep__ external_id — never with per-business coordinates.
func TestGetSharedSession_PinsSentinelCoordinates(t *testing.T) {
	want := &tokenclient.TokenResponse{
		Platform:    a2a.AgentYandexBusiness,
		ExternalID:  tools.YandexSharedRepExternalID,
		AccessToken: `[{"name":"Session_id","value":"shared-secret"}]`,
	}
	var gotExternalID, gotBusiness string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotBusiness = req.URL.Query().Get("business_id")
		gotExternalID = req.URL.Query().Get("external_id")
		require.NoError(t, json.NewEncoder(w).Encode(want))
	}))
	defer srv.Close()

	tc, err := tokenclient.New(srv.URL, nil)
	require.NoError(t, err)
	r := agentbase.NewSharedSessionResolver(agentbase.NewTokenResolver(tc), "shared-biz-uuid")

	cookies, err := r.GetSharedSession(context.Background(), a2a.AgentYandexBusiness, "verify_access")
	require.NoError(t, err)
	assert.Equal(t, want.AccessToken, cookies)
	assert.Equal(t, "shared-biz-uuid", gotBusiness, "must key on the config sentinel business")
	assert.Equal(t, tools.YandexSharedRepExternalID, gotExternalID, "must key on the reserved shared-rep external_id")
}

// TestGetSharedSession_EmptyAccessToken_FailsClosed verifies that a shared row
// present-but-empty (no credential) is treated as not-configured rather than
// returning an empty cookie string that would inject nothing and silently drive
// an unauthenticated session.
func TestGetSharedSession_EmptyAccessToken_FailsClosed(t *testing.T) {
	stub := &stubResolver{info: agentbase.TokenInfo{AccessToken: ""}}
	r := agentbase.NewSharedSessionResolver(stub, "shared-biz")

	_, err := r.GetSharedSession(context.Background(), a2a.AgentYandexBusiness, "verify")
	require.Error(t, err)
	assert.ErrorIs(t, err, agentbase.ErrSharedSessionNotConfigured)
}

// TestGetSharedSession_ExpiredSession_Propagates verifies an expired shared
// session surfaces the coded integration_token_invalid non-retryable error so
// the canary/eviction path treats it the same as a per-business expiry.
func TestGetSharedSession_ExpiredSession_Propagates(t *testing.T) {
	stub := &stubResolver{err: tokenclient.ErrTokenExpired}
	r := agentbase.NewSharedSessionResolver(stub, "shared-biz")

	_, err := r.GetSharedSession(context.Background(), a2a.AgentYandexBusiness, "verify")
	require.Error(t, err)
	assert.Equal(t, "integration_token_invalid", a2a.CodeOf(err))
	var nre *a2a.NonRetryableError
	assert.True(t, errors.As(err, &nre), "expired shared session must be non-retryable")
}
