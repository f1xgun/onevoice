package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// mockPostService implements PostService for tests.
type mockPostService struct {
	scheduleFn   func(context.Context, uuid.UUID, string, string, []string, time.Time) (*domain.Post, error)
	rescheduleFn func(context.Context, uuid.UUID, string, *time.Time, *string) (*domain.Post, error)
	mutateFn     func(context.Context, uuid.UUID, string, string, *string) (*domain.Post, error)
	listFn       func(ctx context.Context, businessID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error)
	getByIDFn    func(ctx context.Context, businessID uuid.UUID, id string) (*domain.Post, error)
}

func (m *mockPostService) List(ctx context.Context, businessID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error) {
	return m.listFn(ctx, businessID, filter)
}

func (m *mockPostService) GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Post, error) {
	return m.getByIDFn(ctx, businessID, id)
}

// postBizCtx seeds a BusinessContext with PermContentRead for post handler tests.
func postBizCtx(businessID, userID uuid.UUID) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermContentRead},
	})
}

func TestNewPostHandler_NilService(t *testing.T) {
	_, err := NewPostHandler(nil)
	require.Error(t, err)
}

func TestListPosts_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockPostService{
		listFn: func(_ context.Context, bid uuid.UUID, f domain.PostFilter) ([]domain.Post, int, error) {
			assert.Equal(t, businessID, bid)
			assert.Equal(t, 20, f.Limit)
			return []domain.Post{{ID: "p1", Content: "test", Status: "published"}}, 1, nil
		},
	}
	h, _ := NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", http.NoBody)
	req = req.WithContext(postBizCtx(businessID, userID))
	rr := httptest.NewRecorder()

	h.ListPosts(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp openapi.PostListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Posts, 1)
	assert.Equal(t, 1, resp.Total)
}

func TestListPosts_WithFilters(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockPostService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.PostFilter) ([]domain.Post, int, error) {
			assert.Equal(t, "telegram", f.Platform)
			assert.Equal(t, "published", f.Status)
			assert.Equal(t, 50, f.Limit)
			assert.Equal(t, 10, f.Offset)
			return nil, 0, nil
		},
	}
	h, _ := NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?platform=telegram&status=published&limit=50&offset=10", http.NoBody)
	req = req.WithContext(postBizCtx(businessID, userID))
	rr := httptest.NewRecorder()

	h.ListPosts(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListPosts_LimitClamped(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockPostService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.PostFilter) ([]domain.Post, int, error) {
			assert.Equal(t, MaxPostLimit, f.Limit, "limit > 100 should be clamped")
			return nil, 0, nil
		},
	}
	h, _ := NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?limit=999", http.NoBody)
	req = req.WithContext(postBizCtx(businessID, userID))
	rr := httptest.NewRecorder()

	h.ListPosts(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListPosts_NoBusinessContext(t *testing.T) {
	h, _ := NewPostHandler(&mockPostService{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListPosts(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestListPosts_BusinessNotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockPostService{
		listFn: func(_ context.Context, _ uuid.UUID, _ domain.PostFilter) ([]domain.Post, int, error) {
			return nil, 0, domain.ErrBusinessNotFound
		},
	}
	h, _ := NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", http.NoBody)
	req = req.WithContext(postBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.ListPosts(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetPost_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockPostService{
		getByIDFn: func(_ context.Context, _ uuid.UUID, id string) (*domain.Post, error) {
			assert.Equal(t, "post-123", id)
			return &domain.Post{ID: "post-123", Content: "test post"}, nil
		},
	}
	h, _ := NewPostHandler(svc)

	r := chi.NewRouter()
	r.Get("/posts/{id}", h.GetPost)

	req := httptest.NewRequest(http.MethodGet, "/posts/post-123", http.NoBody)
	req = req.WithContext(postBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetPost_NotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockPostService{
		getByIDFn: func(_ context.Context, _ uuid.UUID, _ string) (*domain.Post, error) {
			return nil, domain.ErrPostNotFound
		},
	}
	h, _ := NewPostHandler(svc)

	r := chi.NewRouter()
	r.Get("/posts/{id}", h.GetPost)

	req := httptest.NewRequest(http.MethodGet, "/posts/nonexistent", http.NoBody)
	req = req.WithContext(postBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetPost_ServiceError(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockPostService{
		getByIDFn: func(_ context.Context, _ uuid.UUID, _ string) (*domain.Post, error) {
			return nil, fmt.Errorf("database error")
		},
	}
	h, _ := NewPostHandler(svc)

	r := chi.NewRouter()
	r.Get("/posts/{id}", h.GetPost)

	req := httptest.NewRequest(http.MethodGet, "/posts/post-1", http.NoBody)
	req = req.WithContext(postBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func (m *mockPostService) Mutate(ctx context.Context, businessID uuid.UUID, id, action string, content *string) (*domain.Post, error) {
	return m.mutateFn(ctx, businessID, id, action, content)
}

func TestPostMutationHTTPContracts(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		permission               authz.Permission
		owner                    bool
		want                     int
	}{
		{"edit", http.MethodPatch, "/posts/p", `{"content":"new"}`, authz.PermContentUpdate, false, http.StatusOK},
		{"retry", http.MethodPost, "/posts/p/retry", "", authz.PermContentCreate, false, http.StatusOK},
		{"delete owner", http.MethodDelete, "/posts/p", "", authz.PermContentDelete, true, http.StatusOK},
		{"delete admin", http.MethodDelete, "/posts/p", "", authz.PermContentDelete, false, http.StatusForbidden},
		{"invalid edit", http.MethodPatch, "/posts/p", `{}`, authz.PermContentUpdate, false, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			business := uuid.New()
			role := uuid.New()
			if tc.owner {
				role = uuid.MustParse(domain.SystemRoleOwnerID)
			}
			called := false
			svc := &mockPostService{mutateFn: func(_ context.Context, bid uuid.UUID, id, action string, content *string) (*domain.Post, error) {
				called = true
				require.Equal(t, business, bid)
				require.Equal(t, "p", id)
				if action == "edit" {
					require.Equal(t, "new", *content)
				}
				return &domain.Post{ID: id, BusinessID: bid.String(), Status: "published"}, nil
			}}
			h, _ := NewPostHandler(svc)
			router := chi.NewRouter()
			router.Patch("/posts/{id}", h.EditPost)
			router.Delete("/posts/{id}", h.DeletePost)
			router.Post("/posts/{id}/retry", h.RetryPost)
			ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: business, RoleID: role, Permissions: []authz.Permission{tc.permission}})
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)).WithContext(ctx)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.want, rec.Code)
			require.Equal(t, tc.want == http.StatusOK, called)
		})
	}
}

func (m *mockPostService) Schedule(ctx context.Context, business uuid.UUID, target, content string, media []string, at time.Time) (*domain.Post, error) {
	return m.scheduleFn(ctx, business, target, content, media, at)
}
func (m *mockPostService) Reschedule(ctx context.Context, business uuid.UUID, id string, at *time.Time, content *string) (*domain.Post, error) {
	return m.rescheduleFn(ctx, business, id, at, content)
}
