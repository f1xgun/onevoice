package agent_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
)

// fakeTokenFetcher records the last call and returns a preset token.
type fakeTokenFetcher struct {
	token        string
	err          error
	lastBizID    string
	lastPlatform string
}

func (f *fakeTokenFetcher) GetToken(_ context.Context, businessID, platform, _ string) (agent.TokenInfo, error) {
	f.lastBizID = businessID
	f.lastPlatform = platform
	if f.err != nil {
		return agent.TokenInfo{}, f.err
	}
	return agent.TokenInfo{AccessToken: f.token, ExternalID: "12345"}, nil
}

// stubBrowser records operations performed on it.
type stubBrowser struct {
	updatedHours string
	updatedInfo  map[string]string
	repliedID    string
	repliedText  string

	getReviewsFn func(ctx context.Context, limit int) ([]map[string]interface{}, error)
}

func (s *stubBrowser) GetInfo(_ context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{"name": "Test Business", "phone": "+7 999 123 45 67", "status": "Работает"}, nil
}

func (s *stubBrowser) UpdateHours(_ context.Context, hoursJSON string) error {
	s.updatedHours = hoursJSON
	return nil
}

func (s *stubBrowser) UpdateInfo(_ context.Context, info map[string]string) error {
	s.updatedInfo = info
	return nil
}

func (s *stubBrowser) GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if s.getReviewsFn != nil {
		return s.getReviewsFn(ctx, limit)
	}
	return []map[string]interface{}{{"id": "r1", "text": "Great!", "rating": float64(5)}}, nil
}

func (s *stubBrowser) ReplyReview(_ context.Context, reviewID, text string) error {
	s.repliedID = reviewID
	s.repliedText = text
	return nil
}

// stubPool implements agent.BrowserPool for testing.
type stubPool struct {
	browser agent.YandexBrowser
}

func (p *stubPool) ForBusiness(_, _, _ string) agent.YandexBrowser {
	return p.browser
}

func newHandler(fetcher agent.TokenFetcher, browser *stubBrowser) *agent.Handler {
	return agent.NewHandler(fetcher, &stubPool{browser: browser})
}

// --- Happy path tests ---

func TestHandler_UpdateHours_FetchesTokenPerRequest(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies-json-abc"}
	browser := &stubBrowser{}
	h := newHandler(fetcher, browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t1",
		BusinessID: "biz-10",
		Tool:       "yandex_business__update_hours",
		Args:       map[string]interface{}{"hours": `{"mon":"09:00-21:00"}`},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, `{"mon":"09:00-21:00"}`, browser.updatedHours)
	assert.Equal(t, "biz-10", fetcher.lastBizID)
	assert.Equal(t, "yandex_business", fetcher.lastPlatform)
}

func TestHandler_UpdateInfo(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies-json"}
	browser := &stubBrowser{}
	h := newHandler(fetcher, browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t2",
		BusinessID: "biz-11",
		Tool:       "yandex_business__update_info",
		Args: map[string]interface{}{
			"phone":       "+7 999 123 45 67",
			"website":     "https://example.com",
			"description": "Best coffee",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "+7 999 123 45 67", browser.updatedInfo["phone"])
	assert.Equal(t, "https://example.com", browser.updatedInfo["website"])
	assert.Equal(t, "Best coffee", browser.updatedInfo["description"])
}

func TestHandler_GetReviews(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies-json"}
	browser := &stubBrowser{}
	h := newHandler(fetcher, browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t3",
		BusinessID: "biz-12",
		Tool:       "yandex_business__get_reviews",
		Args:       map[string]interface{}{"limit": float64(10)},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	reviews, ok := resp.Result["reviews"].([]map[string]interface{})
	require.True(t, ok)
	assert.Len(t, reviews, 1)
	assert.Equal(t, 1, resp.Result["count"])
}

func TestHandler_GetReviews_DefaultLimit(t *testing.T) {
	var capturedLimit int
	fetcher := &fakeTokenFetcher{token: "cookies-json"}
	browser := &stubBrowser{
		getReviewsFn: func(_ context.Context, limit int) ([]map[string]interface{}, error) {
			capturedLimit = limit
			return nil, nil
		},
	}
	h := newHandler(fetcher, browser)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "yandex_business__get_reviews",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{},
	})
	require.NoError(t, err)
	assert.Equal(t, 20, capturedLimit, "expected default limit 20")
}

func TestHandler_ReplyReview(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies-json"}
	browser := &stubBrowser{}
	h := newHandler(fetcher, browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t4",
		BusinessID: "biz-13",
		Tool:       "yandex_business__reply_review",
		Args: map[string]interface{}{
			"review_id": "r1",
			"text":      "Thanks!",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "r1", browser.repliedID)
	assert.Equal(t, "Thanks!", browser.repliedText)
}

// --- Error tests ---

func TestHandler_TokenFetchError_ReturnsNonRetryable(t *testing.T) {
	fetcher := &fakeTokenFetcher{err: fmt.Errorf("integration not found")}
	browser := &stubBrowser{}
	h := newHandler(fetcher, browser)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID:     "t5",
		BusinessID: "biz-14",
		Tool:       "yandex_business__update_hours",
		Args:       map[string]interface{}{"hours": "{}"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch token")
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "token fetch failure should be NonRetryableError")
}

func TestHandler_UnknownTool(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "tok"}
	browser := &stubBrowser{}
	h := newHandler(fetcher, browser)

	_, err := h.Handle(context.Background(), a2a.ToolRequest{Tool: "yandex_business__unknown"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

// errBrowser is a YandexBrowser that always returns a configured error.
type errBrowser struct {
	err error
}

func (e *errBrowser) GetInfo(_ context.Context) (map[string]interface{}, error) { return nil, e.err }
func (e *errBrowser) UpdateHours(_ context.Context, _ string) error               { return e.err }
func (e *errBrowser) UpdateInfo(_ context.Context, _ map[string]string) error {
	return e.err
}
func (e *errBrowser) GetReviews(_ context.Context, _ int) ([]map[string]interface{}, error) {
	return nil, e.err
}
func (e *errBrowser) ReplyReview(_ context.Context, _, _ string) error { return e.err }

func newErrHandler(fetcher agent.TokenFetcher, browserErr error) *agent.Handler {
	return agent.NewHandler(fetcher, &stubPool{browser: &errBrowser{err: browserErr}})
}

func reviewReq() a2a.ToolRequest {
	return a2a.ToolRequest{
		Tool:       "yandex_business__get_reviews",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"limit": float64(10)},
	}
}

// --- Error classification tests ---

func TestClassifyYandexError_SessionExpired(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies"}
	h := newErrHandler(fetcher, fmt.Errorf("session expired"))

	_, err := h.Handle(context.Background(), reviewReq())
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "session expired should be NonRetryableError")
}

func TestClassifyYandexError_LoginRedirect(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies"}
	h := newErrHandler(fetcher, fmt.Errorf("login redirect detected"))

	_, err := h.Handle(context.Background(), reviewReq())
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "login redirect should be NonRetryableError")
}

func TestClassifyYandexError_Captcha(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies"}
	h := newErrHandler(fetcher, fmt.Errorf("captcha required"))

	_, err := h.Handle(context.Background(), reviewReq())
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "captcha should be NonRetryableError")
}

func TestClassifyYandexError_ReviewNotFound(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies"}
	h := newErrHandler(fetcher, fmt.Errorf("review not found: rev-42"))

	_, err := h.Handle(context.Background(), reviewReq())
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "review not found should be NonRetryableError")
}

func TestClassifyYandexError_ReplyFormUnavailable(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies"}
	h := newErrHandler(fetcher, fmt.Errorf("reply form unavailable for review rev-42"))

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		Tool:       "yandex_business__reply_review",
		BusinessID: "biz-1",
		Args:       map[string]interface{}{"review_id": "rev-42", "text": "Thanks!"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}), "reply form unavailable should be NonRetryableError")
}

func TestClassifyYandexError_PlaywrightTimeout(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies"}
	h := newErrHandler(fetcher, fmt.Errorf("timeout 30000ms exceeded"))

	_, err := h.Handle(context.Background(), reviewReq())
	require.Error(t, err)
	assert.False(t, errors.Is(err, &a2a.NonRetryableError{}), "timeout should NOT be NonRetryableError")
}

func TestClassifyYandexError_TransientNetworkError(t *testing.T) {
	fetcher := &fakeTokenFetcher{token: "cookies"}
	h := newErrHandler(fetcher, fmt.Errorf("net::ERR_CONNECTION_REFUSED"))

	_, err := h.Handle(context.Background(), reviewReq())
	require.Error(t, err)
	assert.False(t, errors.Is(err, &a2a.NonRetryableError{}), "network error should NOT be NonRetryableError")
}
