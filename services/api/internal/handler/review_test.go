package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// mockReviewService implements ReviewService for tests.
type mockReviewService struct {
	listFn        func(ctx context.Context, businessID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error)
	getByIDFn     func(ctx context.Context, businessID uuid.UUID, id string) (*domain.Review, error)
	replyFn       func(ctx context.Context, businessID uuid.UUID, id, replyText string) error
	retryReplyFn  func(ctx context.Context, businessID uuid.UUID, id string) error
	refreshFn     func(ctx context.Context, userID uuid.UUID) error
	batchDraftFn  func(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]service.BatchItemResult, error)
	bulkApproveFn func(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]service.BatchItemResult, error)
	slaFn         func(ctx context.Context, businessID uuid.UUID, targetHours int) (service.SLAStats, error)
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

func (m *mockReviewService) RetryReply(ctx context.Context, businessID uuid.UUID, id string) error {
	if m.retryReplyFn == nil {
		return nil
	}
	return m.retryReplyFn(ctx, businessID, id)
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

func (m *mockReviewService) BatchDraft(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]service.BatchItemResult, error) {
	if m.batchDraftFn == nil {
		return nil, nil
	}
	return m.batchDraftFn(ctx, businessID, reviewIDs)
}

func (m *mockReviewService) BulkApprove(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]service.BatchItemResult, error) {
	if m.bulkApproveFn == nil {
		return nil, nil
	}
	return m.bulkApproveFn(ctx, businessID, reviewIDs)
}

func (m *mockReviewService) SLA(ctx context.Context, businessID uuid.UUID, targetHours int) (service.SLAStats, error) {
	if m.slaFn == nil {
		return service.SLAStats{}, nil
	}
	return m.slaFn(ctx, businessID, targetHours)
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

func TestGetReviewSLA_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		slaFn: func(_ context.Context, gotBiz uuid.UUID, targetHours int) (service.SLAStats, error) {
			assert.Equal(t, businessID, gotBiz, "SLA must be scoped to the caller's business")
			assert.Equal(t, 48, targetHours, "target_hours query param must reach the service")
			return service.SLAStats{
				Total:                       10,
				Unanswered:                  3,
				Answered:                    7,
				Buckets:                     service.UnansweredBuckets{Lt24h: 1, H24to72: 1, Gt72h: 1},
				TargetHours:                 targetHours,
				MedianResponseHours:         5.5,
				AverageResponseHours:        8.25,
				MeasuredResponses:           7,
				PercentAnsweredWithinTarget: 0.75,
			}, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/sla?target_hours=48", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetReviewSLA(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp openapi.ReviewSLAResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, 10, resp.Total)
	assert.Equal(t, 3, resp.Unanswered)
	assert.Equal(t, 7, resp.Answered)
	assert.Equal(t, 1, resp.Buckets.Lt24h)
	assert.Equal(t, 1, resp.Buckets.H24to72)
	assert.Equal(t, 1, resp.Buckets.Gt72h)
	assert.Equal(t, 48, resp.TargetHours)
	assert.InDelta(t, 5.5, resp.MedianResponseHours, 1e-6)
	assert.InDelta(t, 8.25, resp.AverageResponseHours, 1e-6)
	assert.Equal(t, 7, resp.MeasuredResponses)
	assert.InDelta(t, 0.75, resp.PercentAnsweredWithinTarget, 1e-6)
}

func TestGetReviewSLA_DefaultsTargetOnMalformed(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		slaFn: func(_ context.Context, _ uuid.UUID, targetHours int) (service.SLAStats, error) {
			assert.Equal(t, 0, targetHours, "an absent or malformed target_hours reaches the service as 0 (service applies the default)")
			return service.SLAStats{TargetHours: service.SLADefaultTargetHours}, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/sla?target_hours=notanumber", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetReviewSLA(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetReviewSLA_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{},
	})
	h, _ := NewReviewHandler(&mockReviewService{
		slaFn: func(context.Context, uuid.UUID, int) (service.SLAStats, error) {
			t.Fatal("service must not be called without content:read")
			return service.SLAStats{}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/sla", http.NoBody)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.GetReviewSLA(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestGetReviewSLA_BusinessNotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h, _ := NewReviewHandler(&mockReviewService{
		slaFn: func(context.Context, uuid.UUID, int) (service.SLAStats, error) {
			return service.SLAStats{}, domain.ErrBusinessNotFound
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/sla", http.NoBody)
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetReviewSLA(rr, req)

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

// retryReviewRequest runs POST /reviews/{id}/reply/retry through a chi router so
// chi.URLParam resolves, with the supplied BusinessContext on the request.
func retryReviewRequest(ctx context.Context, t *testing.T, svc *mockReviewService) *httptest.ResponseRecorder {
	t.Helper()
	h, err := NewReviewHandler(svc)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Post("/reviews/{id}/reply/retry", h.RetryReviewReply)

	req := httptest.NewRequest(http.MethodPost, "/reviews/rev-1/reply/retry", http.NoBody)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestRetryReviewReply_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	called := false
	svc := &mockReviewService{
		retryReplyFn: func(_ context.Context, biz uuid.UUID, id string) error {
			called = true
			assert.Equal(t, businessID, biz)
			assert.Equal(t, "rev-1", id)
			return nil
		},
	}

	rr := retryReviewRequest(reviewUpdateCtx(businessID, userID), t, svc)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, called, "RetryReply should run for a caller with PermContentUpdate")
}

// TestRetryReviewReply_NotRetryableMapsTo409 pins the state-gate contract on the
// wire: a review that is not in the error reply state must surface as 409 with
// the stable reply_not_retryable code, not as a fake success or a 500.
func TestRetryReviewReply_NotRetryableMapsTo409(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		retryReplyFn: func(_ context.Context, _ uuid.UUID, _ string) error {
			return domain.ErrReviewReplyNotRetryable
		},
	}

	rr := retryReviewRequest(reviewUpdateCtx(businessID, userID), t, svc)

	assert.Equal(t, http.StatusConflict, rr.Code)
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	assert.Equal(t, "reply_not_retryable", body.Code)
}

func TestRetryReviewReply_ReviewNotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		retryReplyFn: func(_ context.Context, _ uuid.UUID, _ string) error {
			return domain.ErrReviewNotFound
		},
	}

	rr := retryReviewRequest(reviewUpdateCtx(businessID, userID), t, svc)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestRetryReviewReply_ViewerForbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		retryReplyFn: func(_ context.Context, _ uuid.UUID, _ string) error {
			t.Fatal("RetryReply must not be invoked for a read-only viewer")
			return nil
		},
	}

	rr := retryReviewRequest(reviewReadCtx(businessID, userID), t, svc)

	assert.Equal(t, http.StatusForbidden, rr.Code)
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

// TestBatchDraftReviews_Success asserts the handler returns the per-item results
// and the succeeded count, and forwards the id list to the service.
func TestBatchDraftReviews_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		batchDraftFn: func(_ context.Context, bid uuid.UUID, ids []string) ([]service.BatchItemResult, error) {
			assert.Equal(t, businessID, bid)
			assert.Equal(t, []string{"r1", "r2"}, ids)
			return []service.BatchItemResult{
				{ReviewID: "r1", Status: service.BatchItemStatusDrafted},
				{ReviewID: "r2", Status: service.BatchItemStatusFailed, Error: "boom"},
			}, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	body := `{"reviewIds":["r1","r2"]}`
	req := httptest.NewRequest(http.MethodPost, "/reviews/batch-draft", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.BatchDraftReviews(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var out batchReviewResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	assert.Len(t, out.Results, 2)
	assert.Equal(t, 1, out.Succeeded, "only the drafted item counts toward succeeded")
	assert.Equal(t, "r2", out.Results[1].ReviewId)
	assert.Equal(t, service.BatchItemStatusFailed, out.Results[1].Status)
}

// TestBatchDraftReviews_OversizedRejected asserts an id list longer than the
// handler cap is rejected with 400 and the service is never called. Revert the
// len>maxBatchReviewIDs guard and this flips to 200 (an unbounded request reaches
// the service).
func TestBatchDraftReviews_OversizedRejected(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	called := false
	svc := &mockReviewService{
		batchDraftFn: func(_ context.Context, _ uuid.UUID, _ []string) ([]service.BatchItemResult, error) {
			called = true
			return nil, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	ids := make([]string, maxBatchReviewIDs+1)
	for i := range ids {
		ids[i] = "r" + strconv.Itoa(i)
	}
	payload, _ := json.Marshal(batchReviewRequest{ReviewIds: ids})
	req := httptest.NewRequest(http.MethodPost, "/reviews/batch-draft", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.BatchDraftReviews(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.False(t, called, "an oversized batch must be rejected before reaching the service")
}

// TestBatchDraftReviews_ViewerForbidden asserts a read-only viewer cannot draft.
func TestBatchDraftReviews_ViewerForbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		batchDraftFn: func(_ context.Context, _ uuid.UUID, _ []string) ([]service.BatchItemResult, error) {
			t.Fatal("batch-draft must not run for a read-only viewer")
			return nil, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/reviews/batch-draft", strings.NewReader(`{"reviewIds":["r1"]}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.BatchDraftReviews(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// TestBulkApproveReviews_Success asserts publish results are returned with the
// succeeded count.
func TestBulkApproveReviews_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		bulkApproveFn: func(_ context.Context, bid uuid.UUID, ids []string) ([]service.BatchItemResult, error) {
			assert.Equal(t, businessID, bid)
			assert.Equal(t, []string{"r1", "r2"}, ids)
			return []service.BatchItemResult{
				{ReviewID: "r1", Status: service.BatchItemStatusPublished},
				{ReviewID: "r2", Status: service.BatchItemStatusSkipped, Error: "not_positive_or_no_draft"},
			}, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/reviews/bulk-approve", strings.NewReader(`{"reviewIds":["r1","r2"]}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.BulkApproveReviews(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var out batchReviewResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	assert.Equal(t, 1, out.Succeeded, "only the published item counts toward succeeded")
	assert.Equal(t, service.BatchItemStatusSkipped, out.Results[1].Status)
}

// TestBulkApproveReviews_EmptyRejected asserts an empty id list is rejected with
// 400 (bulk-approve requires an explicit set).
func TestBulkApproveReviews_EmptyRejected(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	called := false
	svc := &mockReviewService{
		bulkApproveFn: func(_ context.Context, _ uuid.UUID, _ []string) ([]service.BatchItemResult, error) {
			called = true
			return nil, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/reviews/bulk-approve", strings.NewReader(`{"reviewIds":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewUpdateCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.BulkApproveReviews(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.False(t, called, "an empty bulk-approve must not reach the service")
}

// TestBulkApproveReviews_ViewerForbidden asserts a read-only viewer cannot
// bulk-approve.
func TestBulkApproveReviews_ViewerForbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	svc := &mockReviewService{
		bulkApproveFn: func(_ context.Context, _ uuid.UUID, _ []string) ([]service.BatchItemResult, error) {
			t.Fatal("bulk-approve must not run for a read-only viewer")
			return nil, nil
		},
	}
	h, _ := NewReviewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/reviews/bulk-approve", strings.NewReader(`{"reviewIds":["r1"]}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(reviewReadCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.BulkApproveReviews(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}
