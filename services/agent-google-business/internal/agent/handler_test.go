package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
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
	return NewHandler(tokens, func(_ string) GBPClient { return client })
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
