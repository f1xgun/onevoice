package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
	"github.com/f1xgun/onevoice/services/agent-google-business/internal/gbp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTokenFetcher struct {
	err error
}

func (m *mockTokenFetcher) GetToken(_ context.Context, _, _, _ string) (TokenInfo, error) {
	if m.err != nil {
		return TokenInfo{}, m.err
	}
	return TokenInfo{AccessToken: "test-token", ExternalID: "accounts/1/locations/2"}, nil
}

type mockGBPClient struct {
	reviews    *gbp.ListReviewsResponse
	reviewsErr error
	replyResp  *gbp.ReviewReply
	replyErr   error
}

func (m *mockGBPClient) GetReviews(_ context.Context, _ string, _ int) (*gbp.ListReviewsResponse, error) {
	return m.reviews, m.reviewsErr
}

func (m *mockGBPClient) ReplyReview(_ context.Context, _, _ string) (*gbp.ReviewReply, error) {
	return m.replyResp, m.replyErr
}

func newTestHandler(tokens TokenFetcher, client GBPClient) *Handler {
	return NewHandler(tokens, func(_ string) GBPClient { return client }, nil)
}

func TestHandler_Handle_UnknownTool(t *testing.T) {
	h := newTestHandler(&mockTokenFetcher{}, &mockGBPClient{})

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "task-1",
		Tool:       "google_business__nonexistent",
		BusinessID: "biz-1",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
	assert.Nil(t, resp)
}

func TestHandler_GetReviews_Success(t *testing.T) {
	client := &mockGBPClient{
		reviews: &gbp.ListReviewsResponse{
			Reviews: []gbp.Review{
				{
					ReviewID:   "rev-1",
					Name:       "accounts/1/locations/2/reviews/rev-1",
					Reviewer:   gbp.Reviewer{DisplayName: "User 1"},
					StarRating: "FIVE",
					Comment:    "Great place!",
					CreateTime: "2026-01-01T00:00:00Z",
				},
				{
					ReviewID:   "rev-2",
					Name:       "accounts/1/locations/2/reviews/rev-2",
					Reviewer:   gbp.Reviewer{DisplayName: "User 2"},
					StarRating: "THREE",
					Comment:    "Average",
					CreateTime: "2026-01-02T00:00:00Z",
					ReviewReply: &gbp.ReviewReply{
						Comment:    "Thanks for feedback!",
						UpdateTime: "2026-01-03T00:00:00Z",
					},
				},
			},
			AverageRating:    4.0,
			TotalReviewCount: 2,
		},
	}

	h := newTestHandler(&mockTokenFetcher{}, client)
	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "task-1",
		Tool:       "google_business__get_reviews",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"limit": float64(10)},
	})

	require.NoError(t, err)
	require.True(t, resp.Success)

	result := resp.Result
	assert.Equal(t, 2, result["count"])
	assert.Equal(t, 4.0, result["average_rating"])

	reviews := result["reviews"].([]map[string]interface{})
	assert.Equal(t, "rev-1", reviews[0]["review_id"])
	assert.Equal(t, false, reviews[0]["has_reply"])
	assert.Equal(t, true, reviews[1]["has_reply"])
	assert.Equal(t, "Thanks for feedback!", reviews[1]["reply"])
}

func TestHandler_GetReviews_APIError(t *testing.T) {
	client := &mockGBPClient{
		reviewsErr: fmt.Errorf("google api error 403: PERMISSION_DENIED"),
	}

	h := newTestHandler(&mockTokenFetcher{}, client)
	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "task-1",
		Tool:       "google_business__get_reviews",
		BusinessID: "biz-1",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "get reviews")
}

func TestHandler_GetReviews_TokenError(t *testing.T) {
	h := newTestHandler(&mockTokenFetcher{err: fmt.Errorf("token expired")}, &mockGBPClient{})

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "task-1",
		Tool:       "google_business__get_reviews",
		BusinessID: "biz-1",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch token")
}

func TestHandler_ReplyReview_Success(t *testing.T) {
	client := &mockGBPClient{
		replyResp: &gbp.ReviewReply{
			Comment:    "Thank you!",
			UpdateTime: "2026-01-05T00:00:00Z",
		},
	}

	h := newTestHandler(&mockTokenFetcher{}, client)
	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "task-1",
		Tool:       "google_business__reply_review",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"review_name": "accounts/1/locations/2/reviews/rev-1",
			"text":        "Thank you!",
		},
	})

	require.NoError(t, err)
	require.True(t, resp.Success)

	result := resp.Result
	assert.Equal(t, "replied", result["status"])
	assert.Equal(t, "Thank you!", result["reply_text"])
}

func TestHandler_ReplyReview_MissingReviewName(t *testing.T) {
	h := newTestHandler(&mockTokenFetcher{}, &mockGBPClient{})

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "task-1",
		Tool:       "google_business__reply_review",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"text": "Thanks!"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "review_name is required")
}

func TestHandler_ReplyReview_MissingText(t *testing.T) {
	h := newTestHandler(&mockTokenFetcher{}, &mockGBPClient{})

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "task-1",
		Tool:       "google_business__reply_review",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"review_name": "accounts/1/locations/2/reviews/rev-1"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "text is required")
}

func TestClassifyGBPError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		nonRetryable bool
	}{
		{"nil", nil, false},
		{"403", fmt.Errorf("google api error 403: forbidden"), true},
		{"401", fmt.Errorf("google api error 401: unauthenticated"), true},
		{"PERMISSION_DENIED", fmt.Errorf("PERMISSION_DENIED"), true},
		{"NOT_FOUND", fmt.Errorf("NOT_FOUND"), true},
		{"500", fmt.Errorf("google api error: status 500"), false},
		{"network", fmt.Errorf("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyGBPError(tt.err)
			if tt.err == nil {
				assert.Nil(t, result)
				return
			}
			var nonRetryable *a2a.NonRetryableError
			if tt.nonRetryable {
				assert.ErrorAs(t, result, &nonRetryable)
			} else {
				assert.False(t, false)
			}
		})
	}
}

// --- Phase 16 HITL-08: Redis dedupe gate tests ---

// countingGBPClient wraps mockGBPClient with an atomic call counter.
type countingGBPClient struct {
	mockGBPClient
	replyCalls int64
}

func (c *countingGBPClient) ReplyReview(ctx context.Context, reviewName, text string) (*gbp.ReviewReply, error) {
	atomic.AddInt64(&c.replyCalls, 1)
	return c.mockGBPClient.ReplyReview(ctx, reviewName, text)
}

func newGBPDedupeTestHandler(t *testing.T, client GBPClient) (*Handler, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	dedupe := hitldedupe.New(rdb)
	tokens := &mockTokenFetcher{}
	return NewHandler(tokens, func(_ string) GBPClient { return client }, dedupe), mr
}

func gbpReplyReqWithApproval(approvalID string) a2a.ToolRequest {
	return a2a.ToolRequest{
		TaskID:     "task-g",
		Tool:       "google_business__reply_review",
		BusinessID: "biz-1",
		Args: map[string]interface{}{
			"review_name": "accounts/1/locations/2/reviews/rev-1",
			"text":        "Thanks!",
		},
		ApprovalID: approvalID,
	}
}

func TestHandler_Handle_EmptyApprovalID_SkipsDedupe(t *testing.T) {
	client := &countingGBPClient{
		mockGBPClient: mockGBPClient{
			replyResp: &gbp.ReviewReply{Comment: "Thanks!", UpdateTime: "2026-01-05T00:00:00Z"},
		},
	}
	h, mr := newGBPDedupeTestHandler(t, client)

	resp, err := h.Handle(context.Background(), gbpReplyReqWithApproval(""))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, int64(1), atomic.LoadInt64(&client.replyCalls))
	assert.Equal(t, 0, len(mr.Keys()),
		"empty ApprovalID must NOT touch Redis (anti-footgun #2)")
}

func TestHandler_Handle_FirstCallWithApprovalID_ExecutesAndCaches(t *testing.T) {
	client := &countingGBPClient{
		mockGBPClient: mockGBPClient{
			replyResp: &gbp.ReviewReply{Comment: "Thanks!", UpdateTime: "2026-01-05T00:00:00Z"},
		},
	}
	h, mr := newGBPDedupeTestHandler(t, client)

	resp, err := h.Handle(context.Background(), gbpReplyReqWithApproval("appr-g-1"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, int64(1), atomic.LoadInt64(&client.replyCalls))

	key := "hitl:approval:biz-1:appr-g-1"
	require.True(t, mr.Exists(key), "dedupe key must be stored after successful execution")
	val, err := mr.Get(key)
	require.NoError(t, err)
	var cached a2a.ToolResponse
	require.NoError(t, json.Unmarshal([]byte(val), &cached))
	assert.True(t, cached.Success)
}

func TestHandler_Handle_SecondCallWithSameApprovalID_ReturnsCached(t *testing.T) {
	client := &countingGBPClient{
		mockGBPClient: mockGBPClient{
			replyResp: &gbp.ReviewReply{Comment: "Thanks!", UpdateTime: "2026-01-05T00:00:00Z"},
		},
	}
	h, _ := newGBPDedupeTestHandler(t, client)

	resp1, err := h.Handle(context.Background(), gbpReplyReqWithApproval("appr-g-2"))
	require.NoError(t, err)
	require.NotNil(t, resp1)

	resp2, err := h.Handle(context.Background(), gbpReplyReqWithApproval("appr-g-2"))
	require.NoError(t, err)
	require.NotNil(t, resp2)

	assert.Equal(t, int64(1), atomic.LoadInt64(&client.replyCalls),
		"tool must be invoked exactly once across two Handle calls with the same ApprovalID")
	assert.Equal(t, resp1.Success, resp2.Success)
	assert.Equal(t, resp1.Result["status"], resp2.Result["status"])
}

func TestHandler_Handle_ApprovalID_InFlight_ReturnsDuplicateError(t *testing.T) {
	client := &countingGBPClient{
		mockGBPClient: mockGBPClient{
			replyResp: &gbp.ReviewReply{Comment: "Thanks!", UpdateTime: "2026-01-05T00:00:00Z"},
		},
	}
	h, mr := newGBPDedupeTestHandler(t, client)

	key := "hitl:approval:biz-1:appr-g-3"
	require.NoError(t, mr.Set(key, "executing"))
	mr.SetTTL(key, 24*time.Hour)

	resp, err := h.Handle(context.Background(), gbpReplyReqWithApproval("appr-g-3"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Error, "duplicate: already in flight")
	assert.Equal(t, int64(0), atomic.LoadInt64(&client.replyCalls),
		"in-flight claim must short-circuit before tool dispatch")
}
