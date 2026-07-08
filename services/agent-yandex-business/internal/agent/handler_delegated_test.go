package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
)

// delegatedFetcher returns a TokenInfo with an empty AccessToken (the delegated
// signal) and a caller-supplied permalink as the external_id. This models an
// org integration row that carries only a permalink and no credential.
type delegatedFetcher struct {
	permalink string
}

func (d *delegatedFetcher) GetToken(_ context.Context, _, _, _, _ string) (agent.TokenInfo, error) {
	return agent.TokenInfo{AccessToken: "", ExternalID: d.permalink}, nil
}

// recordingShared records the platform/reason it was asked for and returns a
// preset shared session (or error).
type recordingShared struct {
	cookies      string
	err          error
	callCount    int
	lastPlatform string
}

func (s *recordingShared) GetSharedSession(_ context.Context, platform, _ string) (string, error) {
	s.callCount++
	s.lastPlatform = platform
	return s.cookies, s.err
}

// TestHandler_EmptyAccessToken_TakesSharedPath is the connect_mode branch: an
// integration resolving with an EMPTY per-business credential must route through
// the shared representative session (ForSharedBusiness), NOT ForBusiness. It
// also asserts the shared session is bound with the permalink from the resolved
// row. Reverting the empty-token branch in getBrowser makes this fail.
func TestHandler_EmptyAccessToken_TakesSharedPath(t *testing.T) {
	fetcher := &delegatedFetcher{permalink: "114697172504"}
	browser := &stubBrowser{}
	pool := &stubPool{browser: browser}
	shared := &recordingShared{cookies: `[{"name":"Session_id","value":"shared"}]`}

	h := agent.NewHandler(fetcher, pool, nil).WithSharedSession(shared)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-del",
		BusinessID: "biz-A",
		Tool:       tools.YandexBusinessGetReviews,
		Args:       map[string]interface{}{"limit": float64(5)},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.True(t, pool.forSharedCalled, "empty AccessToken must route through the shared (delegated) path")
	assert.False(t, pool.forBusinessCalled, "empty AccessToken must NOT route through the per-business path")
	assert.Equal(t, "114697172504", pool.lastPermalink, "shared browser must be bound with the row's permalink")
	assert.Equal(t, `[{"name":"Session_id","value":"shared"}]`, pool.lastCredential, "shared session cookies must be injected")
	assert.Equal(t, 1, shared.callCount, "shared session must be resolved exactly once")
	assert.Equal(t, a2a.AgentYandexBusiness, shared.lastPlatform)
}

// TestHandler_NonEmptyAccessToken_TakesLegacyPath asserts the additive
// property: a per-business credential (non-empty AccessToken) still routes
// through ForBusiness entirely unchanged, and the shared resolver is never
// consulted.
func TestHandler_NonEmptyAccessToken_TakesLegacyPath(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies-json-legacy"}
	browser := &stubBrowser{}
	pool := &stubPool{browser: browser}
	shared := &recordingShared{cookies: "should-not-be-used"}

	h := agent.NewHandler(fetcher, pool, nil).WithSharedSession(shared)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-legacy",
		BusinessID: "biz-B",
		Tool:       tools.YandexBusinessGetInfo,
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.True(t, pool.forBusinessCalled, "non-empty AccessToken must route through the per-business path")
	assert.False(t, pool.forSharedCalled, "non-empty AccessToken must NOT route through the shared path")
	assert.Equal(t, 0, shared.callCount, "shared resolver must NOT be consulted on the legacy path")
	assert.Equal(t, "cookies-json-legacy", pool.lastCredential)
}

// TestHandler_Delegated_NoSharedResolver_FailsClosed is the fail-closed
// invariant: when an integration resolves to the delegated path but NO shared
// session resolver is wired (delegated plane unprovisioned), the handler must
// return a clear coded, non-retryable "delegated access not configured" error —
// never fall back to ForBusiness with an empty credential.
func TestHandler_Delegated_NoSharedResolver_FailsClosed(t *testing.T) {
	fetcher := &delegatedFetcher{permalink: "114697172504"}
	browser := &stubBrowser{}
	pool := &stubPool{browser: browser}

	h := agent.NewHandler(fetcher, pool, nil) // no WithSharedSession

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-del-unconfigured",
		BusinessID: "biz-A",
		Tool:       tools.YandexBusinessGetReviews,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, agent.ErrDelegatedNotConfigured)
	assert.False(t, pool.forBusinessCalled, "must NOT fall back to per-business path with an empty credential")
	assert.False(t, pool.forSharedCalled, "must NOT bind a shared browser when the plane is unprovisioned")
}

// TestHandler_Delegated_SharedNotConfigured_FailsClosed verifies that when the
// shared resolver itself reports ErrSharedSessionNotConfigured (sentinel unset),
// the handler surfaces the delegated-not-configured error rather than a raw
// resolver error.
func TestHandler_Delegated_SharedNotConfigured_FailsClosed(t *testing.T) {
	fetcher := &delegatedFetcher{permalink: "114697172504"}
	pool := &stubPool{browser: &stubBrowser{}}
	shared := &recordingShared{err: agentbase.ErrSharedSessionNotConfigured}

	h := agent.NewHandler(fetcher, pool, nil).WithSharedSession(shared)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-del-sentinel",
		BusinessID: "biz-A",
		Tool:       tools.YandexBusinessVerifyAccess,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, agent.ErrDelegatedNotConfigured)
	assert.False(t, pool.forSharedCalled)
}

// TestHandler_VerifyAccess_Delegated confirms the verify_access route dispatches
// the delegated shared path and returns the access verdict from the browser.
func TestHandler_VerifyAccess_Delegated(t *testing.T) {
	fetcher := &delegatedFetcher{permalink: "999"}
	pool := &stubPool{browser: &stubBrowser{}} // stubBrowser.VerifyAccess → true
	shared := &recordingShared{cookies: "[]"}

	h := agent.NewHandler(fetcher, pool, nil).WithSharedSession(shared)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t-verify",
		BusinessID: "biz-A",
		Tool:       tools.YandexBusinessVerifyAccess,
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, true, resp.Result["access_verified"])
	assert.True(t, pool.forSharedCalled)
	assert.Equal(t, "999", pool.lastPermalink)
}
