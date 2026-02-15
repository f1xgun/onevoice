package agent

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// YandexBrowser abstracts Playwright browser operations for testability.
type YandexBrowser interface {
	UpdateHours(ctx context.Context, hoursJSON string) error
	UpdateInfo(ctx context.Context, info map[string]string) error
	GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error)
	ReplyReview(ctx context.Context, reviewID, text string) error
}

// Handler implements a2a.Handler for the Yandex.Business RPA agent.
type Handler struct {
	browser YandexBrowser
}

// NewHandler creates a Handler with the given browser automation implementation.
func NewHandler(browser YandexBrowser) *Handler {
	return &Handler{browser: browser}
}

// Handle routes ToolRequests to the appropriate Yandex.Business operation.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "yandex_business__update_hours":
		return h.updateHours(ctx, req)
	case "yandex_business__update_info":
		return h.updateInfo(ctx, req)
	case "yandex_business__get_reviews":
		return h.getReviews(ctx, req)
	case "yandex_business__reply_review":
		return h.replyReview(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

func (h *Handler) updateHours(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	hours, _ := req.Args["hours"].(string)
	if err := h.browser.UpdateHours(ctx, hours); err != nil {
		return nil, fmt.Errorf("yandex: update hours: %w", err)
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated", "note": "changes pending Yandex moderation"},
	}, nil
}

func (h *Handler) updateInfo(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	info := make(map[string]string)
	for _, key := range []string{"phone", "website", "description"} {
		if v, ok := req.Args[key].(string); ok {
			info[key] = v
		}
	}
	if err := h.browser.UpdateInfo(ctx, info); err != nil {
		return nil, fmt.Errorf("yandex: update info: %w", err)
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated", "note": "changes pending Yandex moderation"},
	}, nil
}

func (h *Handler) getReviews(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	limitF, _ := req.Args["limit"].(float64)
	limit := int(limitF)
	if limit == 0 {
		limit = 20
	}

	reviews, err := h.browser.GetReviews(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("yandex: get reviews: %w", err)
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"reviews": reviews, "count": len(reviews)},
	}, nil
}

func (h *Handler) replyReview(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	reviewID, _ := req.Args["review_id"].(string)
	text, _ := req.Args["text"].(string)

	if err := h.browser.ReplyReview(ctx, reviewID, text); err != nil {
		return nil, fmt.Errorf("yandex: reply review: %w", err)
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "replied"},
	}, nil
}
