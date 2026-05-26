package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
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
	// Phase 21-04 + Phase 23.4: nil users / nil pool / nil lock — all middleware
	// gates degrade to pass-through for the router structure tests.
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
