package authz_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// middlewareFakeLoader is a MembershipLoader for middleware tests.
type middlewareFakeLoader struct {
	member    *authz.CachedMember
	role      *authz.CachedRole
	memberErr error
	roleErr   error
}

func (f *middlewareFakeLoader) LoadMembership(_ context.Context, _, _ uuid.UUID) (*authz.CachedMember, error) {
	return f.member, f.memberErr
}

func (f *middlewareFakeLoader) LoadRole(_ context.Context, _ uuid.UUID) (*authz.CachedRole, error) {
	return f.role, f.roleErr
}

// buildRouter wires up a chi router with RequireBusinessAccess and a test handler.
func buildRouter(cache *authz.Cache, extractUserID authz.UserIDExtractor) chi.Router {
	r := chi.NewRouter()
	r.Route("/businesses/{id}", func(r chi.Router) {
		r.Use(authz.RequireBusinessAccess(cache, extractUserID))
		r.Get("/resource", func(w http.ResponseWriter, r *http.Request) {
			bc, ok := authz.BusinessContextFromCtx(r.Context())
			if !ok {
				http.Error(w, "no bc", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(bc)
		})
	})
	return r
}

// makeLoader creates a fake loader returning an active member + role.
func makeLoader(roleID uuid.UUID) *middlewareFakeLoader {
	return &middlewareFakeLoader{
		member: &authz.CachedMember{
			RoleID:   roleID,
			Status:   "active",
			JoinedAt: time.Now(),
		},
		role: &authz.CachedRole{
			Permissions: []authz.Permission{authz.PermContentRead},
		},
	}
}

// fixedUserIDExtractor always returns the given userID.
func fixedUserIDExtractor(userID uuid.UUID) authz.UserIDExtractor {
	return func(_ context.Context) (uuid.UUID, error) {
		return userID, nil
	}
}

// failingUserIDExtractor always returns an error.
func failingUserIDExtractor() authz.UserIDExtractor {
	return func(_ context.Context) (uuid.UUID, error) {
		return uuid.Nil, fmt.Errorf("no user in ctx")
	}
}

// Test 1: Missing/invalid business UUID in URL → 400 with {"error":"invalid_business_id"}
func TestRequireBusinessAccess_InvalidBusinessID(t *testing.T) {
	loader := makeLoader(uuid.New())
	cache := authz.NewCache(loader)
	r := buildRouter(cache, fixedUserIDExtractor(uuid.New()))

	req := httptest.NewRequest(http.MethodGet, "/businesses/not-a-uuid/resource", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "invalid_business_id", body["error"])
}

// Test 2: GetUserID returns error → 401 with {"error":"unauthorized"}
func TestRequireBusinessAccess_NoUserID(t *testing.T) {
	loader := makeLoader(uuid.New())
	cache := authz.NewCache(loader)
	r := buildRouter(cache, failingUserIDExtractor())

	req := httptest.NewRequest(http.MethodGet, "/businesses/"+uuid.New().String()+"/resource", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "unauthorized", body["error"])
}

// Test 3: cache.GetMembership returns ErrMembershipNotFound → 404 with {"error":"not_found"}
func TestRequireBusinessAccess_MembershipNotFound(t *testing.T) {
	loader := &middlewareFakeLoader{
		member:    nil,
		memberErr: domain.ErrMembershipNotFound,
	}
	cache := authz.NewCache(loader)
	r := buildRouter(cache, fixedUserIDExtractor(uuid.New()))

	req := httptest.NewRequest(http.MethodGet, "/businesses/"+uuid.New().String()+"/resource", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "not_found", body["error"])
}

// Test 4: cached member.Status == "suspended" → 403 with {"error":"forbidden_suspended"}
func TestRequireBusinessAccess_SuspendedMember(t *testing.T) {
	roleID := uuid.New()
	loader := &middlewareFakeLoader{
		member: &authz.CachedMember{
			RoleID:   roleID,
			Status:   "suspended",
			JoinedAt: time.Now(),
		},
		role: &authz.CachedRole{
			Permissions: []authz.Permission{authz.PermContentRead},
		},
	}
	cache := authz.NewCache(loader)
	r := buildRouter(cache, fixedUserIDExtractor(uuid.New()))

	req := httptest.NewRequest(http.MethodGet, "/businesses/"+uuid.New().String()+"/resource", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "forbidden_suspended", body["error"])
}

// Test 5: cache.GetRole returns error → 500 with {"error":"internal_server_error"}
func TestRequireBusinessAccess_RoleLoadError(t *testing.T) {
	roleID := uuid.New()
	loader := &middlewareFakeLoader{
		member: &authz.CachedMember{
			RoleID:   roleID,
			Status:   "active",
			JoinedAt: time.Now(),
		},
		role:    nil,
		roleErr: domain.ErrRoleNotFound,
	}
	cache := authz.NewCache(loader)
	r := buildRouter(cache, fixedUserIDExtractor(uuid.New()))

	req := httptest.NewRequest(http.MethodGet, "/businesses/"+uuid.New().String()+"/resource", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "internal_server_error", body["error"])
}

// Test 6: Active member with role → next.ServeHTTP called with BusinessContext injected
func TestRequireBusinessAccess_ActiveMember_InjectsBusinessContext(t *testing.T) {
	roleID := uuid.New()
	userID := uuid.New()
	bizID := uuid.New()
	loader := makeLoader(roleID)
	cache := authz.NewCache(loader)
	r := buildRouter(cache, fixedUserIDExtractor(userID))

	req := httptest.NewRequest(http.MethodGet, "/businesses/"+bizID.String()+"/resource", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var bc authz.BusinessContext
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&bc))
	require.Equal(t, bizID, bc.BusinessID)
	require.Equal(t, userID, bc.UserID)
	require.Equal(t, roleID, bc.RoleID)
	require.Equal(t, []authz.Permission{authz.PermContentRead}, bc.Permissions)
}

// Test 7: Generic loader error (not the sentinel) → 500 with {"error":"internal_server_error"}
func TestRequireBusinessAccess_GenericLoaderError(t *testing.T) {
	loader := &middlewareFakeLoader{
		member:    nil,
		memberErr: fmt.Errorf("database connection reset"),
	}
	cache := authz.NewCache(loader)
	r := buildRouter(cache, fixedUserIDExtractor(uuid.New()))

	req := httptest.NewRequest(http.MethodGet, "/businesses/"+uuid.New().String()+"/resource", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "internal_server_error", body["error"])
}
