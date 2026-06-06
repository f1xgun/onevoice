package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// platformACL is the canonical CN→[]platforms map used by RequirePlatformACL
// behavior tests. Mirrors the documented internal-ACL shape.
func platformACL() map[string][]string {
	return map[string][]string{
		"agent-telegram":        {"telegram"},
		"agent-vk":              {"vk"},
		"agent-yandex-business": {"yandex_business"},
		"agent-google-business": {"google_business"},
		"orchestrator":          {"telegram", "vk", "yandex_business", "google_business"},
		"api":                   {"*"},
	}
}

func runPlatformACL(t *testing.T, cn, platform string) (status int, ran bool, ident string) {
	t.Helper()
	called := false
	var capturedIdentity string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		capturedIdentity = middleware.ServiceIdentityFromContext(r.Context())
	})
	mw := middleware.RequirePlatformACL(platformACL(), nil)(next)

	target := "/internal/v1/tokens?platform=" + platform
	req := newRequestWithPeerCert(http.MethodGet, target, cn)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	return rec.Code, called, capturedIdentity
}

// TestRequirePlatformACL_AgentTelegramOwnPlatform: CN agent-telegram +
// platform=telegram → 200 OK.
func TestRequirePlatformACL_AgentTelegramOwnPlatform(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, identity := runPlatformACL(t, "agent-telegram", "telegram")
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, called, "next handler must run on allowed platform")
	assert.Equal(t, "agent-telegram", identity)
}

// TestRequirePlatformACL_AgentTelegramCrossPlatform: CN agent-telegram +
// platform=yandex_business → 403.
func TestRequirePlatformACL_AgentTelegramCrossPlatform(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, _ := runPlatformACL(t, "agent-telegram", "yandex_business")
	assert.Equal(t, http.StatusForbidden, code)
	assert.False(t, called, "next handler must not run on cross-platform request")
}

// TestRequirePlatformACL_AgentVKCrossPlatform: CN agent-vk + platform=telegram → 403.
func TestRequirePlatformACL_AgentVKCrossPlatform(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, _ := runPlatformACL(t, "agent-vk", "telegram")
	assert.Equal(t, http.StatusForbidden, code)
	assert.False(t, called)
}

// TestRequirePlatformACL_AgentYandexCrossPlatform: CN agent-yandex-business +
// platform=vk → 403.
func TestRequirePlatformACL_AgentYandexCrossPlatform(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, _ := runPlatformACL(t, "agent-yandex-business", "vk")
	assert.Equal(t, http.StatusForbidden, code)
	assert.False(t, called)
}

// TestRequirePlatformACL_AgentGoogleCrossPlatform: CN agent-google-business +
// platform=telegram → 403.
func TestRequirePlatformACL_AgentGoogleCrossPlatform(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, _ := runPlatformACL(t, "agent-google-business", "telegram")
	assert.Equal(t, http.StatusForbidden, code)
	assert.False(t, called)
}

// TestRequirePlatformACL_OrchestratorWildcard: CN orchestrator may request any
// of the four platforms.
func TestRequirePlatformACL_OrchestratorWildcard(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	for _, platform := range []string{"telegram", "vk", "yandex_business", "google_business"} {
		code, called, identity := runPlatformACL(t, "orchestrator", platform)
		assert.Equalf(t, http.StatusOK, code, "orchestrator must reach platform=%s", platform)
		assert.Truef(t, called, "next handler must run for orchestrator platform=%s", platform)
		assert.Equal(t, "orchestrator", identity)
	}
}

// TestRequirePlatformACL_APIWildcard: CN api has "*" → any platform allowed.
func TestRequirePlatformACL_APIWildcard(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, identity := runPlatformACL(t, "api", "anything")
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, called)
	assert.Equal(t, "api", identity)
}

// TestRequirePlatformACL_EmptyPlatform_WildcardCN_Rejected: a wildcard CN with
// NO platform query param is rejected up front (403) so the ACL is the single
// authority on platform shape rather than deferring to the downstream handler.
func TestRequirePlatformACL_EmptyPlatform_WildcardCN_Rejected(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	mw := middleware.RequirePlatformACL(platformACL(), nil)(next)

	req := newRequestWithPeerCert(http.MethodGet, "/internal/v1/tokens", "api")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, called, "wildcard CN with no platform must be rejected before the allow loop")
}

// TestRequirePlatformACL_EmptyPlatform_ScopedCN_Rejected: a scoped CN with no
// platform query param is likewise rejected with 403.
func TestRequirePlatformACL_EmptyPlatform_ScopedCN_Rejected(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, _ := runPlatformACL(t, "agent-telegram", "")
	assert.Equal(t, http.StatusForbidden, code)
	assert.False(t, called, "scoped CN with no platform must be rejected")
}

// TestRequirePlatformACL_UnknownCN: CN not in the ACL map → 403.
func TestRequirePlatformACL_UnknownCN(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, _ := runPlatformACL(t, "unknown-cn", "telegram")
	assert.Equal(t, http.StatusForbidden, code)
	assert.False(t, called, "next handler must not run for unknown CN")
}

// TestRequirePlatformACL_MissingCert_WhenMTLSEnabled: no peer cert and mTLS
// enabled → 403.
func TestRequirePlatformACL_MissingCert_WhenMTLSEnabled(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	mw := middleware.RequirePlatformACL(platformACL(), nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/tokens?platform=telegram", http.NoBody)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, called, "next handler must not run when mtls required but no cert")
}

// TestRequirePlatformACL_MissingCert_WhenMTLSDisabled: no peer cert and mTLS
// disabled → identity resolves to "system"; absent from the ACL map → 403.
func TestRequirePlatformACL_MissingCert_WhenMTLSDisabled(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "false")

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})
	mw := middleware.RequirePlatformACL(platformACL(), nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/tokens?platform=telegram", http.NoBody)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, called, "system identity is not in the ACL map and must be rejected")
}

// TestRequirePlatformACL_SystemAllowedWhenInACL: when mTLS is disabled and the
// ACL explicitly grants the "system" identity, the request proceeds.
func TestRequirePlatformACL_SystemAllowedWhenInACL(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "false")

	acl := map[string][]string{"system": {"telegram"}}
	called := false
	var identity string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		identity = middleware.ServiceIdentityFromContext(r.Context())
	})
	mw := middleware.RequirePlatformACL(acl, nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/tokens?platform=telegram", http.NoBody)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, called)
	assert.Equal(t, "system", identity)
}

// TestRequirePlatformACL_PropagatesIdentity: after a successful allow the CN is
// retrievable via ServiceIdentityFromContext (reused ctx key for the token-decrypt audit).
func TestRequirePlatformACL_PropagatesIdentity(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	code, called, identity := runPlatformACL(t, "orchestrator", "vk")
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, called)
	assert.Equal(t, "orchestrator", identity)
}
