package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/ratelimit"
	"github.com/f1xgun/onevoice/pkg/ssecounter"
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

// TestReviewHandler_Reply_OversizedText_Returns400 asserts the reply length cap:
// a reply far over maxReviewReplyRunes is rejected with 400 and Reply is never
// invoked. Revert the MaxBytesReader line or the rune-count check in
// ReplyToReview and this flips to 200 (the oversized reply reaches the service).
func TestReviewHandler_Reply_OversizedText_Returns400(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	called := false
	svc := &mockReviewService{
		replyFn: func(_ context.Context, _ uuid.UUID, _, _ string) error {
			called = true
			return nil
		},
	}
	h, _ := NewReviewHandler(svc)

	r := chi.NewRouter()
	r.Put("/reviews/{id}/reply", h.ReplyToReview)

	body := `{"replyText":"` + strings.Repeat("a", 100000) + `"}`
	req := httptest.NewRequest(http.MethodPut, "/reviews/rev-1/reply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.False(t, called, "Reply must not run for an oversized reply text")
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

// TestRefreshReviews_ConcurrencyCap_Rejects_WhenUserAtCap proves RefreshReviews
// routes its multi-platform fanout through the same per-user concurrency cap as
// the chat and resume streams. The user is pinned at the cap before the request,
// so Acquire must fail and the handler must return 429 without ever invoking the
// review service's Refresh fanout.
//
// Fail-on-revert: deleting the Acquire/release block in RefreshReviews lets the
// over-cap request fall straight through to Refresh (200), so this flips to 200
// with Refresh invoked and the test fails.
func TestRefreshReviews_ConcurrencyCap_Rejects_WhenUserAtCap(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	called := false
	svc := &mockReviewService{
		refreshFn: func(_ context.Context, _ uuid.UUID) error {
			called = true
			return nil
		},
	}
	h, _ := NewReviewHandler(svc)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h.SetSSECounter(ssecounter.New(rdb, 1, ratelimit.Policy{}), "free")

	if err := mr.Set("sse:user:"+userID.String()+":active", "1"); err != nil {
		t.Fatalf("seed redis cap key: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/refresh", http.NoBody)
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.RefreshReviews(rr, req)

	assert.Equal(t, http.StatusTooManyRequests, rr.Code, "over-cap refresh must be rejected; body=%q", rr.Body.String())
	assert.Equal(t, "1", rr.Header().Get("Retry-After"))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &decoded))
	assert.Equal(t, "sse_concurrency_exceeded", decoded["code"])
	assert.False(t, called, "Refresh must not run when the user is already at the cap")
}

// TestRefreshReviews_ConcurrencyCap_Allows_WhenBelowCap is the companion case:
// with a free slot the refresh acquires it and reaches Refresh, so the cap gates
// only over-budget callers rather than blocking the path outright.
func TestRefreshReviews_ConcurrencyCap_Allows_WhenBelowCap(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	called := false
	svc := &mockReviewService{
		refreshFn: func(_ context.Context, _ uuid.UUID) error {
			called = true
			return nil
		},
	}
	h, _ := NewReviewHandler(svc)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h.SetSSECounter(ssecounter.New(rdb, 1, ratelimit.Policy{}), "free")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/refresh", http.NoBody)
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.RefreshReviews(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "refresh with a free slot must succeed; body=%q", rr.Body.String())
	assert.True(t, called, "Refresh must run when below the cap")
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
