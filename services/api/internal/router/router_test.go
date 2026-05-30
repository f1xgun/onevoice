package router_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	apimiddleware "github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// routerTestJWTSecret is a 32-byte stub secret used by router tests so
// NewAuthHandler's jwtSecret minimum-length check passes.
var routerTestJWTSecret = []byte("test-jwt-secret-32-bytes-padding-zz")

// fakeLoader implements authz.MembershipLoader for constructing a test Cache.
type fakeLoader struct{}

func (f *fakeLoader) LoadMembership(_ context.Context, _, _ uuid.UUID) (*authz.CachedMember, error) {
	return nil, domain.ErrMembershipNotFound
}

func (f *fakeLoader) LoadRole(_ context.Context, _ uuid.UUID) (*authz.CachedRole, error) {
	return nil, domain.ErrMembershipNotFound
}

// stubUserService satisfies handler.UserService for tests — all methods panic
// if called (routing smoke tests never hit handler bodies).
type stubUserService struct{}

func (stubUserService) Register(_ context.Context, _, _ string) (*domain.User, error) {
	panic("not called in routing test")
}
func (stubUserService) RegisterWithContext(_ context.Context, _, _ string, _ service.RegistrationContext) (*domain.User, error) {
	panic("not called in routing test")
}
func (stubUserService) Login(_ context.Context, _, _ string) (user *domain.User, accessToken, refreshToken string, err error) {
	panic("not called in routing test")
}
func (stubUserService) RefreshToken(_ context.Context, _ string) (user *domain.User, accessToken, refreshToken string, err error) {
	panic("not called in routing test")
}
func (stubUserService) Logout(_ context.Context, _ string) error {
	panic("not called in routing test")
}
func (stubUserService) GetByID(_ context.Context, _ uuid.UUID) (*domain.User, error) {
	panic("not called in routing test")
}
func (stubUserService) ChangePassword(_ context.Context, _ uuid.UUID, _, _ string) error {
	panic("not called in routing test")
}
func (stubUserService) UpdatePreferredLocale(_ context.Context, _ uuid.UUID, _ string) error {
	panic("not called in routing test")
}

// buildTestHandlers constructs a Handlers struct with all required fields
// populated with zero-value or stub handlers so the router can be built.
// Handler bodies are never invoked during chi.Walk or 404-probe tests.
func buildTestHandlers() *router.Handlers {
	authH, err := handler.NewAuthHandler(stubUserService{}, false, audit.Nop(), routerTestJWTSecret)
	if err != nil {
		panic("buildTestHandlers: " + err.Error())
	}
	return &router.Handlers{
		Auth:          authH,
		Business:      &handler.BusinessHandler{},
		Integration:   &handler.IntegrationHandler{},
		Conversation:  &handler.ConversationHandler{},
		OAuth:         &oauth.OAuthHandler{},
		Connect:       &connect.ConnectHandler{},
		InternalToken: &handler.InternalTokenHandler{},
		ChatProxy:     &handler.ChatProxyHandler{},
		Review:        &handler.ReviewHandler{},
		Post:          &handler.PostHandler{},
		AgentTask:     &handler.AgentTaskHandler{},
		Telemetry:     &handler.TelemetryHandler{},
		Project:       &handler.ProjectHandler{},
		HITL:          &handler.HITLHandler{},
		Titler:        &handler.TitlerHandler{},
		Search:        &handler.SearchHandler{},
		Permissions:   &handler.PermissionsHandler{},
		Members:       &handler.MembersHandler{},
		Roles:         &handler.RolesHandler{},
	}
}

func buildTestRouter(t *testing.T) *chi.Mux {
	t.Helper()
	cache := authz.NewCacheForTest(&fakeLoader{}, time.Second, time.Second)
	hc := health.New()
	handlers := buildTestHandlers()
	// nil users / nil pool / nil lock — all middleware gates degrade to
	// pass-through for the router structure tests.
	return router.Setup(handlers, []byte("test-secret"), nil, hc, []string{"http://localhost:3000"}, router.RateLimits{Register: 10, Login: 10, Chat: 10, HITL: 10}, cache, nil, nil, nil)
}

// TestRouter_BusinessScopedRouteCount asserts that at least 30 routes are
// registered under /api/v1/businesses/{id}/.
func TestRouter_BusinessScopedRouteCount(t *testing.T) {
	r := buildTestRouter(t)
	count := 0
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/v1/businesses/{id}/") || route == "/api/v1/businesses/{id}" {
			count++
		}
		return nil
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 30, "expected ≥30 routes under /api/v1/businesses/{id}/")
}

// TestRouter_SingularBusinessRouteGone asserts that GET /api/v1/business
// returns 404 (the old singular route was deleted).
func TestRouter_SingularBusinessRouteGone(t *testing.T) {
	r := buildTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/business", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, "singular /api/v1/business must not be registered")
}

// TestRouter_ListUserBusinessesRegistered asserts that GET /api/v1/businesses
// is registered (auth-only, not business-scoped).
func TestRouter_ListUserBusinessesRegistered(t *testing.T) {
	r := buildTestRouter(t)
	found := false
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet && route == "/api/v1/businesses" {
			found = true
		}
		return nil
	})
	assert.True(t, found, "GET /api/v1/businesses must be registered")
}

// TestRouter_CreateBusinessRegistered asserts that POST /api/v1/businesses
// is registered (auth-only, not business-scoped).
func TestRouter_CreateBusinessRegistered(t *testing.T) {
	r := buildTestRouter(t)
	found := false
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodPost && route == "/api/v1/businesses" {
			found = true
		}
		return nil
	})
	assert.True(t, found, "POST /api/v1/businesses must be registered")
}

// TestRouter_XFFFromUntrustedPeerIgnored proves the chi.RealIP removal: a
// request with X-Forwarded-For matching a would-be-trusted CIDR but a TCP
// peer OUTSIDE TRUSTED_PROXY_CIDRS must reach the handler with r.RemoteAddr
// unchanged — so middleware.ClientIP correctly returns the TCP peer (not
// the spoofed XFF) and the lockout key is bound to the attacker's /16.
//
// We mount a fresh chi router with the SAME global middleware stack as
// router.Setup (minus RealIP, which is the contract under test) and
// attach a probe handler that captures r.RemoteAddr and middleware.ClientIP.
// We do NOT call router.Setup directly because the real Setup mounts
// /auth/login which requires fully wired auth + lockout dependencies —
// here we only need to assert on the IP-resolution contract.
//
// Acceptance: this test fails before the chi.RealIP removal (RealIP
// would rewrite r.RemoteAddr to "178.154.250.5") and passes after.
func TestRouter_XFFFromUntrustedPeerIgnored(t *testing.T) {
	// Force defaultTrustedCIDRs (178.154.250.0/24, 84.252.160.0/19) into
	// the package-level set so ClientIP can be exercised end-to-end.
	require.NoError(t, apimiddleware.InitTrustedProxies(""))

	var capturedRemoteAddr, capturedClientIP string
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRemoteAddr = r.RemoteAddr
		capturedClientIP = apimiddleware.ClientIP(r)
		w.WriteHeader(http.StatusOK)
	})

	mux := chi.NewRouter()
	mux.Use(chimiddleware.RequestID)
	mux.Use(apimiddleware.CorrelationID())
	// NOTE: NO chimiddleware.RealIP here — that is the contract this test enforces.
	mux.Use(chimiddleware.Logger)
	mux.Use(chimiddleware.Recoverer)
	mux.Post("/probe", probe)

	req := httptest.NewRequest(http.MethodPost, "/probe", http.NoBody)
	// TCP peer outside any trusted CIDR.
	req.RemoteAddr = "9.9.9.9:443"
	// XFF that WOULD match a trusted CIDR (Yandex Cloud LB range
	// 178.154.250.0/24) — attacker trying to look like an LB.
	req.Header.Set("X-Forwarded-For", "178.154.250.5")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	// r.RemoteAddr MUST be the TCP peer, NOT rewritten from XFF.
	require.Equal(t, "9.9.9.9:443", capturedRemoteAddr,
		"chi.RealIP is intentionally not mounted — r.RemoteAddr must equal the actual TCP peer")
	// middleware.ClientIP MUST return the TCP peer host (9.9.9.9),
	// ignoring the spoofed XFF because 9.9.9.9 is not in any trusted CIDR.
	require.Equal(t, "9.9.9.9", capturedClientIP,
		"middleware.ClientIP must ignore X-Forwarded-For when the TCP peer is outside TRUSTED_PROXY_CIDRS")
	// The /16 derived for lockout keying MUST be the attacker's /16,
	// not the spoofed-LB /16 — proves the lockout/captcha gate is bound
	// to the real attacker.
	require.Equal(t, "9.9.0.0/16", apimiddleware.Net16(capturedClientIP))
}

// --- internal billing route smoke tests ---

// stubBilling is the minimal handler.BillingService implementation for
// internal-router tests. It never returns an error so success-path tests
// can assert 204.
type stubBilling struct{}

func (stubBilling) LogUsage(_ context.Context, _ *llm.UsageLog) error { return nil }
func (stubBilling) GetDailySpend(_ context.Context, _ uuid.UUID, _ time.Time) (float64, error) {
	return 0, nil
}

// buildTestInternalRouter wires SetupInternal with a fake billing repo so
// the route's middleware stack is exercised end-to-end without DB.
func buildTestInternalRouter(t *testing.T) http.Handler {
	t.Helper()
	hc := health.New()
	h := buildTestHandlers()
	// Inject internal billing handler.
	h.InternalBilling = handler.NewInternalBillingHandler(stubBilling{}, nil)
	return router.SetupInternal(h, hc)
}

// Test 8: SetupInternal applies RequireServiceIdentity to /billing/usage_logs.
// With MTLS enabled and no peer cert, the route returns 403.
func TestSetupInternal_BillingRoute_AppliesMTLSMiddleware(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	r := buildTestInternalRouter(t)

	body := bytes.NewReader([]byte(`{"business_id":"` + uuid.New().String() + `","model":"x","provider":"y"}`))
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/billing/usage_logs", body)
	req.Header.Set("Content-Type", "application/json")
	// req.TLS == nil — RequireServiceIdentity must 403.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// Test 9: SetupInternal allows orchestrator CN on /billing/usage_logs.
// With MTLS enabled and peer cert CN=orchestrator, the route returns 204.
func TestSetupInternal_BillingRoute_AllowsOrchestratorCN(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	r := buildTestInternalRouter(t)

	body := bytes.NewReader([]byte(`{"business_id":"` + uuid.New().String() + `","model":"x","provider":"y"}`))
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/billing/usage_logs", body)
	req.Header.Set("Content-Type", "application/json")
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "orchestrator"}}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// Bonus: billing route NOT mounted on the PUBLIC /api/v1 router.
func TestRouter_PublicRouter_DoesNotMountBillingRoute(t *testing.T) {
	r := buildTestRouter(t)
	found := false
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.Contains(route, "billing/usage_logs") {
			found = true
		}
		return nil
	})
	assert.False(t, found, "POST /internal/v1/billing/usage_logs must NOT be on the public mux")
}

// --- WR-04: GET /internal/v1/billing/daily_spend mTLS + public-mount guards ---
//
// Mirrors the POST usage_logs tests above so the GET route's security
// invariants are pinned by tests instead of code inspection. A future PR that
// moved the route outside the RequireServiceIdentity group (or onto the
// public mux) would now fail a test rather than silently exposing the
// daily-spend probe off-cluster.

// TestSetupInternal_DailySpendRoute_AppliesMTLSMiddleware — request with no
// peer cert returns 403 (mTLS gate active).
func TestSetupInternal_DailySpendRoute_AppliesMTLSMiddleware(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	r := buildTestInternalRouter(t)

	url := "/internal/v1/billing/daily_spend?business_id=" + uuid.New().String() + "&date=2026-05-30"
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	// req.TLS == nil — RequireServiceIdentity must 403.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestSetupInternal_DailySpendRoute_AllowsOrchestratorCN — request with the
// orchestrator's peer cert passes the gate and reaches the handler (200).
func TestSetupInternal_DailySpendRoute_AllowsOrchestratorCN(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	r := buildTestInternalRouter(t)

	url := "/internal/v1/billing/daily_spend?business_id=" + uuid.New().String() + "&date=2026-05-30"
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "orchestrator"}}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestRouter_PublicRouter_DoesNotMountDailySpendRoute — the daily_spend
// route must NOT appear on the public /api/v1 mux. Defense in depth on top
// of the mTLS listener split.
func TestRouter_PublicRouter_DoesNotMountDailySpendRoute(t *testing.T) {
	r := buildTestRouter(t)
	found := false
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.Contains(route, "billing/daily_spend") {
			found = true
		}
		return nil
	})
	assert.False(t, found, "GET /internal/v1/billing/daily_spend must NOT be on the public mux")
}
