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

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// mockReviewService implements ReviewService for tests.
type mockReviewService struct {
	listFn    func(ctx context.Context, userID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error)
	getByIDFn func(ctx context.Context, userID uuid.UUID, id string) (*domain.Review, error)
	replyFn   func(ctx context.Context, userID uuid.UUID, id, replyText string) error
}

func (m *mockReviewService) List(ctx context.Context, userID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error) {
	return m.listFn(ctx, userID, filter)
}

func (m *mockReviewService) GetByID(ctx context.Context, userID uuid.UUID, id string) (*domain.Review, error) {
	return m.getByIDFn(ctx, userID, id)
}

func (m *mockReviewService) Reply(ctx context.Context, userID uuid.UUID, id, replyText string) error {
	return m.replyFn(ctx, userID, id, replyText)
}

func TestNewReviewHandler_NilService(t *testing.T) {
	_, err := NewReviewHandler(nil)
	require.Error(t, err)
}

func TestListReviews_Success(t *testing.T) {
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	h.ListReviews(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp ReviewListResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Reviews, 1)
	assert.Equal(t, 5, resp.Reviews[0].Rating)
}

func TestListReviews_Unauthorized(t *testing.T) {
	h, _ := NewReviewHandler(&mockReviewService{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListReviews(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestListReviews_LimitClamped(t *testing.T) {
	userID := uuid.New()
	svc := &mockReviewService{
		listFn: func(_ context.Context, _ uuid.UUID, f domain.ReviewFilter) ([]domain.Review, int, error) {
			assert.Equal(t, MaxReviewLimit, f.Limit)
			return nil, 0, nil
		},
	}
	h, _ := NewReviewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews?limit=500", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	h.ListReviews(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetReview_Success(t *testing.T) {
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetReview_NotFound(t *testing.T) {
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestReplyToReview_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockReviewService{
		replyFn: func(_ context.Context, uid uuid.UUID, id, text string) error {
			assert.Equal(t, userID, uid)
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestReplyToReview_EmptyText(t *testing.T) {
	userID := uuid.New()
	h, _ := NewReviewHandler(&mockReviewService{})

	r := chi.NewRouter()
	r.Put("/reviews/{id}/reply", h.ReplyToReview)

	body := `{"replyText":""}`
	req := httptest.NewRequest(http.MethodPut, "/reviews/rev-1/reply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestReplyToReview_InvalidJSON(t *testing.T) {
	userID := uuid.New()
	h, _ := NewReviewHandler(&mockReviewService{})

	r := chi.NewRouter()
	r.Put("/reviews/{id}/reply", h.ReplyToReview)

	req := httptest.NewRequest(http.MethodPut, "/reviews/rev-1/reply", strings.NewReader("not json"))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestReplyToReview_ReviewNotFound(t *testing.T) {
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestReplyToReview_ServiceError(t *testing.T) {
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
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}
