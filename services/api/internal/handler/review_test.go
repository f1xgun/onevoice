package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// mockReviewService implements ReviewService for tests.
type mockReviewService struct {
	listFn    func(ctx context.Context, businessID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error)
	getByIDFn func(ctx context.Context, businessID uuid.UUID, id string) (*domain.Review, error)
	replyFn   func(ctx context.Context, businessID uuid.UUID, id, replyText string) error
	refreshFn func(ctx context.Context, userID uuid.UUID) error
}

func (m *mockReviewService) List(ctx context.Context, businessID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error) {
	return m.listFn(ctx, businessID, filter)
}

func (m *mockReviewService) GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Review, error) {
	return m.getByIDFn(ctx, businessID, id)
}

func (m *mockReviewService) Reply(ctx context.Context, businessID uuid.UUID, id, replyText string) error {
	return m.replyFn(ctx, businessID, id, replyText)
}

// reviewReadCtx seeds a BusinessContext with PermContentRead for review read tests.
func reviewReadCtx(businessID, userID uuid.UUID) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermContentRead},
	})
}

// reviewUpdateCtx seeds a BusinessContext with PermContentUpdate for reply tests.
func reviewUpdateCtx(businessID, userID uuid.UUID) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermContentRead, authz.PermContentUpdate},
	})
}

func (m *mockReviewService) Refresh(ctx context.Context, userID uuid.UUID) error {
	if m.refreshFn == nil {
		return nil
	}
	return m.refreshFn(ctx, userID)
}

func TestNewReviewHandler_NilService(t *testing.T) {
	_, err := NewReviewHandler(nil)
	require.Error(t, err)
}

func TestListReviews_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.ReviewFilter) ([]domain.Review, int, error) {
			assert.Equal(t, "vk", f.Platform)
			assert.Equal(t, "pending", f.ReplyStatus)
			return []domain.Review{{ID: "r1", Rating: 5, Text: "Great!"}}, 1, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews?platform=vk&reply_status=pending", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.ListReviews(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp openapi.ReviewListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Reviews, 1)
	assert.Equal(t, 5, resp.Reviews[0].Rating)
}

func TestListReviews_NoBusinessContext(t *testing.T) {
	h, _ := NewReviewHandler(&mockReviewService{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListReviews(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestListReviews_LimitClamped(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.ReviewFilter) ([]domain.Review, int, error) {
			assert.Equal(t, MaxReviewLimit, f.Limit)
			return nil, 0, nil
		},
	}
	h, _ := NewReviewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews?limit=500", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.ListReviews(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetReview_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		getByIDFn: func(_ context.Context, _ uuid.UUID, id string) (*domain.Review, error) {
			return &domain.Review{ID: id, Rating: 4, Text: "Good"}, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	r := chi.NewRouter()
	r.Get("/reviews/{id}", h.GetReview)

	req := httptest.NewRequest(http.MethodGet, "/reviews/rev-1", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetReview_NotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		getByIDFn: func(_ context.Context, _ uuid.UUID, _ string) (*domain.Review, error) {
			return nil, domain.ErrReviewNotFound
		},
	}
	h, _ := NewReviewHandler(svc)

	r := chi.NewRouter()
	r.Get("/reviews/{id}", h.GetReview)

	req := httptest.NewRequest(http.MethodGet, "/reviews/nonexistent", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestReplyToReview_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		replyFn: func(_ context.Context, bid uuid.UUID, id, text string) error {
			assert.Equal(t, businessID, bid)
			assert.Equal(t, "rev-1", id)
			assert.Equal(t, "Thank you!", text)
			return nil
		},
	}
	h, _ := NewReviewHandler(svc)

	r := chi.NewRouter()
	r.Put("/reviews/{id}/reply", h.ReplyToReview)

	body := `{"replyText":"Thank you!"}`
	req := httptest.NewRequest(http.MethodPut, "/reviews/rev-1/reply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestReplyToReview_EmptyText(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h, _ := NewReviewHandler(&mockReviewService{})

	r := chi.NewRouter()
	r.Put("/reviews/{id}/reply", h.ReplyToReview)

	body := `{"replyText":""}`
	req := httptest.NewRequest(http.MethodPut, "/reviews/rev-1/reply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestReplyToReview_InvalidJSON(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h, _ := NewReviewHandler(&mockReviewService{})

	r := chi.NewRouter()
	r.Put("/reviews/{id}/reply", h.ReplyToReview)

	req := httptest.NewRequest(http.MethodPut, "/reviews/rev-1/reply", strings.NewReader("not json"))
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestReplyToReview_ReviewNotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		replyFn: func(_ context.Context, _ uuid.UUID, _, _ string) error {
			return domain.ErrReviewNotFound
		},
	}
	h, _ := NewReviewHandler(svc)

	r := chi.NewRouter()
	r.Put("/reviews/{id}/reply", h.ReplyToReview)

	body := `{"replyText":"Thanks"}`
	req := httptest.NewRequest(http.MethodPut, "/reviews/rev-1/reply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestRefreshReviews_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	called := false
	svc := &mockReviewService{
		refreshFn: func(_ context.Context, uid uuid.UUID) error {
			called = true
			assert.Equal(t, userID, uid)
			return nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/refresh", http.NoBody)
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.RefreshReviews(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, called, "Refresh should run for a caller with PermContentUpdate")
}

func TestRefreshReviews_ViewerForbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		refreshFn: func(_ context.Context, _ uuid.UUID) error {
			t.Fatal("Refresh must not be invoked for a read-only viewer")
			return nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/refresh", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.RefreshReviews(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "forbidden")
}

func TestReplyToReview_ServiceError(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		replyFn: func(_ context.Context, _ uuid.UUID, _, _ string) error {
			return fmt.Errorf("database error")
		},
	}
	h, _ := NewReviewHandler(svc)

	r := chi.NewRouter()
	r.Put("/reviews/{id}/reply", h.ReplyToReview)

	body := `{"replyText":"Thanks"}`
	req := httptest.NewRequest(http.MethodPut, "/reviews/rev-1/reply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
