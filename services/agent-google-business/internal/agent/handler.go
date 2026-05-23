package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tools"

	"github.com/f1xgun/onevoice/services/agent-google-business/internal/gbp"
)

// TokenInfo aliases agentbase.TokenInfo so existing test mocks compile.
// ExternalID is the GBP location resource name
// (e.g. "accounts/X/locations/Y").
type TokenInfo = agentbase.TokenInfo

// TokenFetcher aliases agentbase.TokenResolver — kept for test
// compatibility (import-path/wiring-only changes in handler_test.go).
type TokenFetcher = agentbase.TokenResolver

// GBPClient abstracts Google Business Profile API operations for testability.
type GBPClient interface {
	GetReviews(ctx context.Context, locationName string, limit int) (*gbp.ListReviewsResponse, error)
	ReplyReview(ctx context.Context, reviewName, comment string) (*gbp.ReviewReply, error)
}

// GBPClientFactory creates a GBP client from an access token.
type GBPClientFactory func(accessToken string) GBPClient

// Handler is the Google Business agent's per-request processor. Its
// Handle method satisfies a2a.Exec and is wired into a2a.NewAgent from
// cmd/main.go.
type Handler struct {
	tokens        TokenFetcher
	clientFactory GBPClientFactory
	dispatcher    agentbase.Dispatcher
}

// NewHandler creates a Handler with per-request token fetching and an
// agentbase.Dispatcher (HITL dedupe gate + error classification). A nil
// dispatcher disables HITL and applies classification directly — used by
// unit tests and dev-local environments without Redis.
func NewHandler(tokens TokenFetcher, factory GBPClientFactory, dispatcher agentbase.Dispatcher) *Handler {
	return &Handler{tokens: tokens, clientFactory: factory, dispatcher: dispatcher}
}

// Handle routes the ToolRequest to the appropriate GBP API operation via the
// agentbase.Dispatcher. When dispatcher is nil we route directly through
// routeTool and apply ClassifyGBPError once.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	if h.dispatcher == nil {
		resp, err := h.routeTool(ctx, req)
		return resp, ClassifyGBPError(err)
	}
	return h.dispatcher.Dispatch(ctx, req, h.routeTool)
}

// routeTool dispatches a ToolRequest to the per-tool implementation. The
// dispatcher (in Handle) handles dedupe + classification around this exec.
func (h *Handler) routeTool(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case tools.GoogleBusinessGetReviews:
		return h.getReviews(ctx, req)
	case tools.GoogleBusinessReplyReview:
		return h.replyReview(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

// ClassifyGBPError is the exported entry point used by cmd/main.go to wire
// the dispatcher's classifier via agentbase.FuncClassifier. Body unchanged.
func ClassifyGBPError(err error) error {
	return classifyGBPError(err)
}

// classifyGBPError wraps permanent Google API errors as NonRetryableError.
func classifyGBPError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "401") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "PERMISSION_DENIED") || strings.Contains(msg, "UNAUTHENTICATED") {
		return a2a.NewNonRetryableError(err)
	}
	if strings.Contains(msg, "404") || strings.Contains(msg, "NOT_FOUND") {
		return a2a.NewNonRetryableError(err)
	}
	return err
}

func (h *Handler) getClient(ctx context.Context, req a2a.ToolRequest) (GBPClient, string, error) {
	info, err := h.tokens.GetToken(ctx, req.BusinessID, a2a.AgentGoogleBusiness, "")
	if err != nil {
		return nil, "", a2a.NewNonRetryableError(fmt.Errorf("fetch token: %w", err))
	}
	client := h.clientFactory(info.AccessToken)
	return client, info.ExternalID, nil
}

func (h *Handler) getReviews(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	limitF, _ := req.Args["limit"].(float64)
	limit := int(limitF)
	if limit == 0 {
		limit = gbp.DefaultReviewLimit
	}

	client, locationName, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetReviews(ctx, locationName, limit)
	if err != nil {
		return nil, fmt.Errorf("google_business: get reviews: %w", classifyGBPError(err))
	}

	reviews := make([]map[string]interface{}, 0, len(resp.Reviews))
	for _, r := range resp.Reviews {
		review := map[string]interface{}{
			"review_id":  r.ReviewID,
			"name":       r.Name,
			"author":     r.Reviewer.DisplayName,
			"rating":     r.StarRating,
			"comment":    r.Comment,
			"created_at": r.CreateTime,
			"has_reply":  r.ReviewReply != nil,
		}
		if r.ReviewReply != nil {
			review["reply"] = r.ReviewReply.Comment
		}
		reviews = append(reviews, review)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result: map[string]interface{}{
			"reviews":        reviews,
			"count":          len(reviews),
			"average_rating": resp.AverageRating,
			"total_count":    resp.TotalReviewCount,
		},
	}, nil
}

func (h *Handler) replyReview(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	reviewName, _ := req.Args["review_name"].(string)
	text, _ := req.Args["text"].(string)

	if reviewName == "" {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("review_name is required"))
	}
	if text == "" {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("text is required"))
	}

	client, _, err := h.getClient(ctx, req)
	if err != nil {
		return nil, err
	}

	reply, err := client.ReplyReview(ctx, reviewName, text)
	if err != nil {
		return nil, fmt.Errorf("google_business: reply review: %w", classifyGBPError(err))
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result: map[string]interface{}{
			"status":     "replied",
			"reply_text": reply.Comment,
			"updated_at": reply.UpdateTime,
		},
	}, nil
}
