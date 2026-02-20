package agent_test

import (
	"context"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubBrowser struct {
	updatedHours string
	updatedInfo  map[string]string
	repliedID    string
	repliedText  string
}

func (s *stubBrowser) UpdateHours(_ context.Context, hoursJSON string) error {
	s.updatedHours = hoursJSON
	return nil
}

func (s *stubBrowser) UpdateInfo(_ context.Context, info map[string]string) error {
	s.updatedInfo = info
	return nil
}

func (s *stubBrowser) GetReviews(_ context.Context, _ int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"id": "r1", "text": "Отличное место!", "rating": float64(5)}}, nil
}

func (s *stubBrowser) ReplyReview(_ context.Context, reviewID, text string) error {
	s.repliedID = reviewID
	s.repliedText = text
	return nil
}

func TestHandler_UpdateHours(t *testing.T) {
	browser := &stubBrowser{}
	h := agent.NewHandler(browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t1",
		Tool:   "yandex_business__update_hours",
		Args:   map[string]interface{}{"hours": `{"mon":"09:00-21:00"}`},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, `{"mon":"09:00-21:00"}`, browser.updatedHours)
}

func TestHandler_UpdateInfo(t *testing.T) {
	browser := &stubBrowser{}
	h := agent.NewHandler(browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t2",
		Tool:   "yandex_business__update_info",
		Args: map[string]interface{}{
			"phone":       "+7 999 123 45 67",
			"website":     "https://example.com",
			"description": "Best coffee",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "+7 999 123 45 67", browser.updatedInfo["phone"])
}

func TestHandler_GetReviews(t *testing.T) {
	browser := &stubBrowser{}
	h := agent.NewHandler(browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t3",
		Tool:   "yandex_business__get_reviews",
		Args:   map[string]interface{}{"limit": float64(10)},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	reviews, ok := resp.Result["reviews"].([]map[string]interface{})
	require.True(t, ok)
	assert.Len(t, reviews, 1)
	assert.Equal(t, 1, resp.Result["count"])
}

func TestHandler_ReplyReview(t *testing.T) {
	browser := &stubBrowser{}
	h := agent.NewHandler(browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t4",
		Tool:   "yandex_business__reply_review",
		Args: map[string]interface{}{
			"review_id": "r1",
			"text":      "Спасибо за отзыв!",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "r1", browser.repliedID)
	assert.Equal(t, "Спасибо за отзыв!", browser.repliedText)
}

func TestHandler_UnknownTool(t *testing.T) {
	h := agent.NewHandler(&stubBrowser{})
	_, err := h.Handle(context.Background(), a2a.ToolRequest{Tool: "yandex_business__unknown"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}
