package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// mockPostService implements PostService for tests.
type mockPostService struct {
	listFn    func(ctx context.Context, userID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error)
	getByIDFn func(ctx context.Context, userID uuid.UUID, id string) (*domain.Post, error)
}

func (m *mockPostService) List(ctx context.Context, userID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error) {
	return m.listFn(ctx, userID, filter)
}

func (m *mockPostService) GetByID(ctx context.Context, userID uuid.UUID, id string) (*domain.Post, error) {
	return m.getByIDFn(ctx, userID, id)
}

func TestNewPostHandler_NilService(t *testing.T) {
	_, err := NewPostHandler(nil)
	require.Error(t, err)
}

func TestListPosts_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockPostService{
		listFn: func(_ context.Context, uid uuid.UUID, f domain.PostFilter) ([]domain.Post, int, error) {
			assert.Equal(t, userID, uid)
			assert.Equal(t, 20, f.Limit) // default
			return []domain.Post{{ID: "p1", Content: "test", Status: "published"}}, 1, nil
		},
	}
	h, _ := NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()

	h.ListPosts(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp PostListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Posts, 1)
	assert.Equal(t, 1, resp.Total)
}

func TestListPosts_WithFilters(t *testing.T) {
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()

	h.ListPosts(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListPosts_LimitClamped(t *testing.T) {
	userID := uuid.New()
	svc := &mockPostService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.PostFilter) ([]domain.Post, int, error) {
			assert.Equal(t, MaxPostLimit, f.Limit, "limit > 100 should be clamped")
			return nil, 0, nil
		},
	}
	h, _ := NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?limit=999", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()

	h.ListPosts(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestListPosts_Unauthorized(t *testing.T) {
	h, _ := NewPostHandler(&mockPostService{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListPosts(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestListPosts_BusinessNotFound(t *testing.T) {
	userID := uuid.New()
	svc := &mockPostService{
		listFn: func(_ context.Context, _ uuid.UUID, _ domain.PostFilter) ([]domain.Post, int, error) {
			return nil, 0, domain.ErrBusinessNotFound
		},
	}
	h, _ := NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	h.ListPosts(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetPost_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockPostService{
		getByIDFn: func(_ context.Context, uid uuid.UUID, id string) (*domain.Post, error) {
			assert.Equal(t, "post-123", id)
			return &domain.Post{ID: "post-123", Content: "test post"}, nil
		},
	}
	h, _ := NewPostHandler(svc)

	r := chi.NewRouter()
	r.Get("/posts/{id}", h.GetPost)

	req := httptest.NewRequest(http.MethodGet, "/posts/post-123", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetPost_NotFound(t *testing.T) {
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetPost_ServiceError(t *testing.T) {
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
