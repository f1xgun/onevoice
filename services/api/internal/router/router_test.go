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

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/llm"
	authservice "github.com/f1xgun/onevoice/services/api/internal/auth"
	"github.com/f1xgun/onevoice/services/api/internal/config"
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

func (stubUserService) UpdateName(_ context.Context, _ uuid.UUID, _ string) error {
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

// setupRouterTestRedis returns a go-redis client backed by an in-process
// miniredis so router tests can exercise rate-limit middleware wiring without
// a real Redis server.
func setupRouterTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return client, mr
}

func buildTestRouter(t *testing.T) *chi.Mux {
	t.Helper()
	cache := authz.NewCacheForTest(&fakeLoader{}, time.Second, time.Second)
	hc := health.New()
	handlers := buildTestHandlers()
	return router.Setup(handlers, []byte("test-secret"), nil, hc, []string{"http://localhost:3000"}, router.RateLimits{Register: 10, Login: 10, Chat: 10, HITL: 10, Writes: 1000, Invitations: 1000}, cache, nil, nil, nil)
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

// TestRouter_SearchRouteRateLimited asserts that GET /api/v1/businesses/{id}/search
// carries a route-level rate-limit middleware on top of its business-scoped
// middleware stack. The /search handler fans out into regex scans over scoped messages,
// so leaving it un-rate-limited is a DoS amplification vector. We compare the
// search route's middleware chain length against a sibling business-scoped GET
// route that has no per-route limiter (GET /conversations): the search route
// must have strictly more middlewares. Reverting the .With(RateLimitByUser)
// wrapper equalizes the counts and fails this test.
func TestRouter_SearchRouteRateLimited(t *testing.T) {
	redisClient, _ := setupRouterTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	cache := authz.NewCacheForTest(&fakeLoader{}, time.Second, time.Second)
	hc := health.New()
	handlers := buildTestHandlers()
	r := router.Setup(handlers, []byte("test-secret"), redisClient, hc,
		[]string{"http://localhost:3000"},
		router.RateLimits{Register: 10, Login: 10, Chat: 10, HITL: 10, Writes: 1000, Invitations: 1000, Search: 1},
		cache, nil, nil, nil)

	var searchMW, baselineMW int
	var searchFound, baselineFound bool
	err := chi.Walk(r, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet && route == "/api/v1/businesses/{id}/search" {
			searchFound = true
			searchMW = len(mws)
		}
		if method == http.MethodGet && route == "/api/v1/businesses/{id}/conversations" {
			baselineFound = true
			baselineMW = len(mws)
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, searchFound, "GET /api/v1/businesses/{id}/search must be registered")
	require.True(t, baselineFound, "baseline GET /conversations route must exist")
	assert.Greater(t, searchMW, baselineMW,
		"search route must carry an extra rate-limit middleware vs. an un-limited sibling GET")
}

// permissiveLoader grants every (business, user) pair an active membership on
// a role carrying PermContentCreate so RequireBusinessAccess admits the request
// and the HITL resume route's rate-limit middleware is actually reached.
type permissiveLoader struct{ roleID uuid.UUID }

func (l *permissiveLoader) LoadMembership(_ context.Context, _, _ uuid.UUID) (*authz.CachedMember, error) {
	return &authz.CachedMember{RoleID: l.roleID, Status: "active"}, nil
}

func (l *permissiveLoader) LoadRole(_ context.Context, _ uuid.UUID) (*authz.CachedRole, error) {
	return &authz.CachedRole{Permissions: []authz.Permission{authz.PermContentCreate}}, nil
}

// mintAccessToken signs a valid access JWT for userID so requests pass the
// Auth middleware mounted on the business-scoped group.
func mintAccessToken(t *testing.T, secret []byte, userID uuid.UUID) string {
	t.Helper()
	claims := authservice.AccessTokenClaims{
		UserID: userID,
		Email:  "limiter@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    authservice.TokenIssuer,
			Subject:   authservice.TokenSubjectAccess,
			Audience:  jwt.ClaimStrings{authservice.TokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	require.NoError(t, err)
	return signed
}

// replayResumeChain rebuilds the HITL resume route's exact route-scoped
// middleware chain (as registered by router.Setup, including the route-level
// rate-limit middleware) onto a fresh chi router terminating in a benign 200
// handler, so the real limiter wiring is exercised without invoking the
// production handler body. The outer-group Auth middleware is prepended
// explicitly (chi.Walk does not report parent-group Use middlewares) so the
// captured RequireBusinessAccess middleware receives an authenticated userID.
// The route is mounted under /api/v1/businesses/{id} so RequireBusinessAccess
// resolves the business UUID from the "id" param exactly as in production; the
// inner conversation segment is renamed to avoid chi's duplicate-param-key
// rejection on a flat pattern.
func replayResumeChain(t *testing.T, src *chi.Mux, jwtSecret []byte) *chi.Mux {
	t.Helper()
	const wantPattern = "/api/v1/businesses/{id}/chat/{id}/resume"
	var chain []func(http.Handler) http.Handler
	var found bool
	err := chi.Walk(src, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if method == http.MethodPost && route == wantPattern {
			found = true
			chain = mws
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, found, "route %s must be registered", wantPattern)

	terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux := chi.NewRouter()
	mux.Use(apimiddleware.Auth(jwtSecret))
	mux.Route("/api/v1/businesses/{id}", func(r chi.Router) {
		r.With(chain...).Post("/chat/{convID}/resume", terminal)
	})
	return mux
}

// TestRouter_HITLResumeHasDistinctRateLimitScope is the fail-on-revert guard
// for the chat/HITL rate-limit decoupling. The chat route and the HITL resume
// route must key their per-user rate-limit buckets on DISTINCT Redis scopes so
// RATE_LIMIT_CHAT and RATE_LIMIT_HITL govern independent counters. Here we
// exhaust the chat-scope bucket for a user, then drive a request through the
// resume route's real middleware chain: with distinct scopes the resume bucket
// is untouched and the request passes its limiter (200). Reverting the resume
// route back to scope "chat" makes it share the now-exhausted chat counter, so
// the resume request is throttled (429) and this test fails.
func TestRouter_HITLResumeHasDistinctRateLimitScope(t *testing.T) {
	require.NoError(t, apimiddleware.InitTrustedProxies(""))

	redisClient, _ := setupRouterTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	jwtSecret := []byte("test-jwt-secret-32-bytes-padding-zz")
	cache := authz.NewCacheForTest(&permissiveLoader{roleID: uuid.New()}, time.Minute, time.Minute)
	hc := health.New()
	handlers := buildTestHandlers()
	const chatLimit, hitlLimit = 1, 1
	r := router.Setup(handlers, jwtSecret, redisClient, hc,
		[]string{"http://localhost:3000"},
		router.RateLimits{Register: 10, Login: 10, Chat: chatLimit, HITL: hitlLimit, Writes: 1000, Invitations: 1000},
		cache, nil, nil, nil)

	resume := replayResumeChain(t, r, jwtSecret)

	userID := uuid.New()
	token := mintAccessToken(t, jwtSecret, userID)

	chatKey := "ratelimit:user:" + userID.String() + ":chat"
	for i := 0; i <= chatLimit; i++ {
		require.NoError(t, redisClient.Incr(context.Background(), chatKey).Err())
	}
	require.NoError(t, redisClient.Expire(context.Background(), chatKey, time.Minute).Err())

	bizID := uuid.New()
	convID := uuid.New()
	url := "/api/v1/businesses/" + bizID.String() + "/chat/" + convID.String() + "/resume?batch_id=b1"
	req := httptest.NewRequest(http.MethodPost, url, http.NoBody)
	req.RemoteAddr = "203.0.113.99:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	resume.ServeHTTP(rec, req)

	assert.NotEqual(t, http.StatusTooManyRequests, rec.Code,
		"HITL resume must use its own rate-limit scope: exhausting the chat bucket must not throttle resume")
	assert.Equal(t, http.StatusOK, rec.Code,
		"resume request must pass auth, authz and its own (fresh) rate-limit bucket")

	hitlKey := "ratelimit:user:" + userID.String() + ":hitl"
	require.Equal(t, int64(1), redisClient.Exists(context.Background(), hitlKey).Val(),
		"resume route must increment the distinct hitl-scope key, not the chat key")
}

// replayLogoChain rebuilds the PUT /logo route's exact route-scoped middleware
// chain (as registered by router.Setup, including the per-user writeLimit)
// onto a fresh chi router terminating in a benign 200 handler, so the real
// limiter wiring is exercised without invoking the production handler body
// (which streams to object storage). The outer-group Auth middleware is
// prepended explicitly (chi.Walk does not report parent-group Use middlewares)
// so the captured RequireBusinessAccess middleware receives an authenticated
// userID, and the route is mounted under /api/v1/businesses/{id} so the
// business UUID resolves from the "id" param exactly as in production.
func replayLogoChain(t *testing.T, src *chi.Mux, jwtSecret []byte) *chi.Mux {
	t.Helper()
	const wantPattern = "/api/v1/businesses/{id}/logo"
	var chain []func(http.Handler) http.Handler
	var found bool
	err := chi.Walk(src, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if method == http.MethodPut && route == wantPattern {
			found = true
			chain = mws
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, found, "route %s must be registered", wantPattern)

	terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux := chi.NewRouter()
	mux.Use(apimiddleware.Auth(jwtSecret))
	mux.Route("/api/v1/businesses/{id}", func(r chi.Router) {
		r.With(chain...).Put("/logo", terminal)
	})
	return mux
}

// TestRouter_LogoUploadRateLimited is the fail-on-revert guard for the logo
// write rate-limit: the
// PUT /logo route streams to object storage and fans out to every connected
// platform via SyncBusiness, so it must carry the per-user writeLimit like its
// siblings (PUT /, integration connect/refresh). Here we drive Writes+1
// requests for one user through the route's real middleware chain: the first
// Writes pass (200) and the (Writes+1)th is throttled (429). Reverting the
// .With(writeLimit) wrapper lets every request through (200), failing the
// final assertion.
func TestRouter_LogoUploadRateLimited(t *testing.T) {
	require.NoError(t, apimiddleware.InitTrustedProxies(""))

	redisClient, _ := setupRouterTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	jwtSecret := []byte("test-jwt-secret-32-bytes-padding-zz")
	cache := authz.NewCacheForTest(&permissiveLoader{roleID: uuid.New()}, time.Minute, time.Minute)
	hc := health.New()
	handlers := buildTestHandlers()
	const writesLimit = 3
	r := router.Setup(handlers, jwtSecret, redisClient, hc,
		[]string{"http://localhost:3000"},
		router.RateLimits{Register: 10, Login: 10, Chat: 10, HITL: 10, Writes: writesLimit, Invitations: 1000},
		cache, nil, nil, nil)

	logo := replayLogoChain(t, r, jwtSecret)

	userID := uuid.New()
	token := mintAccessToken(t, jwtSecret, userID)
	bizID := uuid.New()
	url := "/api/v1/businesses/" + bizID.String() + "/logo"

	var lastCode int
	for i := 0; i <= writesLimit; i++ {
		req := httptest.NewRequest(http.MethodPut, url, http.NoBody)
		req.RemoteAddr = "203.0.113.10:5555"
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		logo.ServeHTTP(rec, req)
		lastCode = rec.Code
		if i < writesLimit {
			require.Equal(t, http.StatusOK, rec.Code,
				"request %d/%d (within budget) must pass the writeLimit", i+1, writesLimit)
		}
	}
	assert.Equal(t, http.StatusTooManyRequests, lastCode,
		"the (Writes+1)th PUT /logo from one user must be throttled by writeLimit")
}

// replayRegenerateTitleChain rebuilds the POST
// /conversations/{id}/regenerate-title route's exact route-scoped middleware
// chain (including the per-user writeLimit) onto a fresh chi router terminating
// in a benign 200 handler. Mirrors replayLogoChain: the outer-group Auth
// middleware is prepended explicitly (chi.Walk does not report parent-group Use
// middlewares) and the route is mounted under /api/v1/businesses/{id}.
func replayRegenerateTitleChain(t *testing.T, src *chi.Mux, jwtSecret []byte) *chi.Mux {
	t.Helper()
	const wantPattern = "/api/v1/businesses/{id}/conversations/{id}/regenerate-title"
	var chain []func(http.Handler) http.Handler
	var found bool
	err := chi.Walk(src, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if method == http.MethodPost && route == wantPattern {
			found = true
			chain = mws
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, found, "route %s must be registered", wantPattern)

	terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux := chi.NewRouter()
	mux.Use(apimiddleware.Auth(jwtSecret))
	mux.Route("/api/v1/businesses/{id}", func(r chi.Router) {
		r.With(chain...).Post("/conversations/{id}/regenerate-title", terminal)
	})
	return mux
}

// TestRouter_RegenerateTitleRateLimited is the fail-on-revert guard for the
// auto-title write rate-limit: POST /conversations/{id}/regenerate-title spends
// a best-effort LLM call on the ungated "background" tier (which the per-business
// daily-spend gate does not bound), so an authenticated user must not be able to
// flood it. It carries the per-user writeLimit like its write siblings. Here we
// drive Writes+1 requests for one user through the route's real middleware
// chain: the first Writes pass (200) and the (Writes+1)th is throttled (429).
// Reverting the .With(writeLimit) wrapper lets every request through (200),
// failing the final assertion.
func TestRouter_RegenerateTitleRateLimited(t *testing.T) {
	require.NoError(t, apimiddleware.InitTrustedProxies(""))

	redisClient, _ := setupRouterTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	jwtSecret := []byte("test-jwt-secret-32-bytes-padding-zz")
	cache := authz.NewCacheForTest(&permissiveLoader{roleID: uuid.New()}, time.Minute, time.Minute)
	hc := health.New()
	handlers := buildTestHandlers()
	const writesLimit = 3
	r := router.Setup(handlers, jwtSecret, redisClient, hc,
		[]string{"http://localhost:3000"},
		router.RateLimits{Register: 10, Login: 10, Chat: 10, HITL: 10, Writes: writesLimit, Invitations: 1000},
		cache, nil, nil, nil)

	regen := replayRegenerateTitleChain(t, r, jwtSecret)

	userID := uuid.New()
	token := mintAccessToken(t, jwtSecret, userID)
	bizID := uuid.New()
	convID := uuid.New()
	url := "/api/v1/businesses/" + bizID.String() + "/conversations/" + convID.String() + "/regenerate-title"

	var lastCode int
	for i := 0; i <= writesLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, url, http.NoBody)
		req.RemoteAddr = "203.0.113.11:5555"
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		regen.ServeHTTP(rec, req)
		lastCode = rec.Code
		if i < writesLimit {
			require.Equal(t, http.StatusOK, rec.Code,
				"request %d/%d (within budget) must pass the writeLimit", i+1, writesLimit)
		}
	}
	assert.Equal(t, http.StatusTooManyRequests, lastCode,
		"the (Writes+1)th regenerate-title from one user must be throttled by writeLimit")
}

// replayResetRequestChain rebuilds the public POST /auth/password-reset/request
// route's middleware chain (the per-IP RateLimit for password-reset) onto a fresh
// chi router terminating in a benign 200 handler, so the real limiter wiring is
// exercised without invoking the production handler body (which would send a
// real reset email). This route lives at the top of /api/v1 with no parent Use
// middleware, so the captured chain is mounted directly.
func replayResetRequestChain(t *testing.T, src *chi.Mux) *chi.Mux {
	t.Helper()
	return replayPostChain(t, src, "/api/v1/auth/password-reset/request")
}

func replayPostChain(t *testing.T, src *chi.Mux, wantPattern string) *chi.Mux {
	t.Helper()
	var chain []func(http.Handler) http.Handler
	var found bool
	err := chi.Walk(src, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if method == http.MethodPost && route == wantPattern {
			found = true
			chain = mws
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, found, "route %s must be registered", wantPattern)

	terminal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux := chi.NewRouter()
	mux.With(chain...).Post(wantPattern, terminal)
	return mux
}

// TestRouter_PasswordResetRequestRateLimited is the fail-on-revert guard for
// the password-reset per-IP rate-limit: the unauthenticated
// POST /auth/password-reset/request route sends a
// real reset email per known address, so it must carry a per-IP RateLimit like
// its siblings (/auth/register, /auth/login). The per-EMAIL throttle does not
// cap aggregate outbound mail from one source across DISTINCT addresses. Here
// we fire Register+1 requests from one RemoteAddr through the route's real
// middleware chain: the first Register pass (200) and the (Register+1)th is
// throttled (429). Reverting the per-IP RateLimit wrapper lets every request
// through (200), failing the final assertion.
func TestRouter_PasswordResetRequestRateLimited(t *testing.T) {
	require.NoError(t, apimiddleware.InitTrustedProxies(""))

	redisClient, _ := setupRouterTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	cache := authz.NewCacheForTest(&fakeLoader{}, time.Minute, time.Minute)
	hc := health.New()
	handlers := buildTestHandlers()
	const registerLimit = 3
	r := router.Setup(handlers, []byte("test-secret"), redisClient, hc,
		[]string{"http://localhost:3000"},
		router.RateLimits{Register: registerLimit, Login: 10, Chat: 10, HITL: 10, Writes: 1000, Invitations: 1000},
		cache, nil, nil, nil)

	reset := replayResetRequestChain(t, r)

	url := "/api/v1/auth/password-reset/request"
	var lastCode int
	for i := 0; i <= registerLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, url, http.NoBody)
		req.RemoteAddr = "198.51.100.7:4444"
		rec := httptest.NewRecorder()
		reset.ServeHTTP(rec, req)
		lastCode = rec.Code
		if i < registerLimit {
			require.Equal(t, http.StatusOK, rec.Code,
				"request %d/%d (within budget) must pass the per-IP RateLimit", i+1, registerLimit)
		}
	}
	assert.Equal(t, http.StatusTooManyRequests, lastCode,
		"the (Register+1)th POST /auth/password-reset/request from one IP must be throttled")
}

func TestRouter_RefreshUsesItsOwnRateLimitBudget(t *testing.T) {
	require.NoError(t, apimiddleware.InitTrustedProxies(""))

	redisClient, _ := setupRouterTestRedis(t)
	defer func() { _ = redisClient.Close() }()

	cache := authz.NewCacheForTest(&fakeLoader{}, time.Minute, time.Minute)
	hc := health.New()
	handlers := buildTestHandlers()
	const (
		testOrigin   = "http://localhost:3000"
		loginLimit   = 2
		refreshLimit = 5
	)
	r := router.Setup(handlers, []byte("test-secret"), redisClient, hc,
		[]string{testOrigin},
		router.RateLimits{
			Register: 10, Login: loginLimit, Refresh: refreshLimit,
			Chat: 10, HITL: 10, Writes: 1000, Invitations: 1000,
		},
		cache, nil, nil, nil)

	refresh := replayPostChain(t, r, "/api/v1/auth/refresh")

	var lastCode int
	for i := 0; i <= refreshLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", http.NoBody)
		req.RemoteAddr = "198.51.100.9:5555"
		rec := httptest.NewRecorder()
		refresh.ServeHTTP(rec, req)
		lastCode = rec.Code
		if i < refreshLimit {
			require.Equal(t, http.StatusOK, rec.Code,
				"refresh %d/%d must pass — the route must not spend the Login budget (%d)",
				i+1, refreshLimit, loginLimit)
		}
	}
	assert.Equal(t, http.StatusTooManyRequests, lastCode,
		"the (Refresh+1)th POST /auth/refresh from one IP must still be throttled")
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
	mux.Use(chimiddleware.Logger)
	mux.Use(chimiddleware.Recoverer)
	mux.Post("/probe", probe)

	req := httptest.NewRequest(http.MethodPost, "/probe", http.NoBody)
	req.RemoteAddr = "9.9.9.9:443"
	req.Header.Set("X-Forwarded-For", "178.154.250.5")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "9.9.9.9:443", capturedRemoteAddr,
		"chi.RealIP is intentionally not mounted — r.RemoteAddr must equal the actual TCP peer")
	require.Equal(t, "9.9.9.9", capturedClientIP,
		"middleware.ClientIP must ignore X-Forwarded-For when the TCP peer is outside TRUSTED_PROXY_CIDRS")
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

// testInternalACL is the CN→[]platforms map wired into SetupInternal by the
// internal-router tests so the /internal/v1/tokens platform gate is exercised.
func testInternalACL() map[string][]string {
	return map[string][]string{
		"agent-telegram": {"telegram"},
		"orchestrator":   {"telegram", "vk", "yandex_business", "google_business"},
		"api":            {"*"},
	}
}

// buildTestInternalRouter wires SetupInternal with a fake billing repo so
// the route's middleware stack is exercised end-to-end without DB.
func buildTestInternalRouter(t *testing.T) http.Handler {
	t.Helper()
	hc := health.New()
	h := buildTestHandlers()
	h.InternalBilling = handler.NewInternalBillingHandler(stubBilling{}, nil)
	cfg := &config.Config{InternalACL: testInternalACL()}
	return router.SetupInternal(h, hc, cfg)
}

// TestInternalTokens_ACL_RejectsCrossPlatform — CN agent-telegram requesting
// platform=yandex_business is gated by RequirePlatformACL before the handler
// runs, returning 403.
func TestInternalTokens_ACL_RejectsCrossPlatform(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	r := buildTestInternalRouter(t)

	url := "/internal/v1/tokens?platform=yandex_business&business_id=" + uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "agent-telegram"}}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestInternalTokens_ACL_AllowsOwnPlatform — CN agent-telegram requesting
// platform=telegram passes the ACL gate and reaches the handler. With
// business_id omitted the handler returns 400, proving the middleware allowed
// the request through.
func TestInternalTokens_ACL_AllowsOwnPlatform(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	r := buildTestInternalRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/tokens?platform=telegram", http.NoBody)
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "agent-telegram"}}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"request must reach the handler (400 for missing business_id), not be ACL-rejected")
}

// TestInternalTokens_ACL_RejectsUnknownCN — a CN absent from the ACL map is
// rejected before reaching the handler.
func TestInternalTokens_ACL_RejectsUnknownCN(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	r := buildTestInternalRouter(t)

	url := "/internal/v1/tokens?platform=telegram&business_id=" + uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "attacker"}}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// Test 8: SetupInternal applies RequireServiceIdentity to /billing/usage_logs.
// With MTLS enabled and no peer cert, the route returns 403.
func TestSetupInternal_BillingRoute_AppliesMTLSMiddleware(t *testing.T) {
	t.Setenv("ONEVOICE_MTLS_ENABLED", "true")
	r := buildTestInternalRouter(t)

	body := bytes.NewReader([]byte(`{"business_id":"` + uuid.New().String() + `","model":"x","provider":"y"}`))
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/billing/usage_logs", body)
	req.Header.Set("Content-Type", "application/json")
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
