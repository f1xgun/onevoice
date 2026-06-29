package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
// cmd/main.go. The dispatch chain (dispatcher fallback + per-tool routing +
// "unknown tool" error) lives in agentbase.NewRouter.
type Handler struct {
	tokens        TokenFetcher
	clientFactory GBPClientFactory
	exec          agentbase.ToolExec
}

// NewHandler creates a Handler with per-request token fetching and an
// agentbase.Dispatcher (HITL dedupe gate + error classification). A nil
// dispatcher disables HITL — on that path the router applies ClassifyGBPError
// as the fallback classifier.
func NewHandler(tokens TokenFetcher, factory GBPClientFactory, dispatcher agentbase.Dispatcher) *Handler {
	h := &Handler{tokens: tokens, clientFactory: factory}
	h.exec = agentbase.NewRouter(h.routes(), dispatcher, agentbase.FuncClassifier(ClassifyGBPError))
	return h
}

// Handle is the a2a.Exec entry point — a thin shim over the router built in
// NewHandler.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	return h.exec(ctx, req)
}

// routes binds the Google Business tool catalog to the Handler's per-tool
// methods.
func (h *Handler) routes() map[string]agentbase.ToolExec {
	return map[string]agentbase.ToolExec{
		tools.GoogleBusinessGetReviews:  h.getReviews,
		tools.GoogleBusinessReplyReview: h.replyReview,
	}
}

// ClassifyGBPError is the exported entry point used by cmd/main.go to wire
// the dispatcher's classifier via agentbase.FuncClassifier. Body unchanged.
func ClassifyGBPError(err error) error {
	return classifyGBPError(err)
}

// classifyGBPError stamps permanent Google API errors with a typed Code and
// composes with NonRetryableError. 429/RESOURCE_EXHAUSTED (and a 403 quota
// error) map to rate_limit_exceeded; a genuine 401/403/PERMISSION_DENIED/
// UNAUTHENTICATED maps to integration_token_invalid; 404/NOT_FOUND map to
// channel_not_found; everything else is treated as transient (retryable).
// A typed *gbp.APIError is classified on its numeric Code/Status; only when no
// typed error is present does it fall back to substring matching.
func classifyGBPError(err error) error {
	if err == nil {
		return nil
	}
	if a2a.CodeOf(err) != "" {
		return err
	}

	var apiErr *gbp.APIError
	if errors.As(err, &apiErr) {
		return classifyGBPAPIError(err, apiErr)
	}

	msg := err.Error()
	if isRateLimitMessage(msg) {
		return a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(err))
	}
	if strings.Contains(msg, "401") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "PERMISSION_DENIED") || strings.Contains(msg, "UNAUTHENTICATED") {
		return a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(err))
	}
	if strings.Contains(msg, "404") || strings.Contains(msg, "NOT_FOUND") {
		return a2a.NewCodedError("channel_not_found", a2a.NewNonRetryableError(err))
	}
	return a2a.NewCodedError("transient", err)
}

// classifyGBPAPIError classifies a typed Google API error on its numeric Code
// and canonical Status. The rate-limit branch is checked before the auth
// branch so a 429/RESOURCE_EXHAUSTED (or 403 quota) condition is reported as a
// transient rate_limit_exceeded rather than a permanent reconnect prompt.
func classifyGBPAPIError(err error, apiErr *gbp.APIError) error {
	if apiErr.Code == 429 || apiErr.Status == "RESOURCE_EXHAUSTED" ||
		(apiErr.Code == 403 && isRateLimitMessage(apiErr.Status+" "+apiErr.Message)) {
		return a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(err))
	}
	if apiErr.Code == 401 || apiErr.Code == 403 ||
		apiErr.Status == "PERMISSION_DENIED" || apiErr.Status == "UNAUTHENTICATED" {
		return a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(err))
	}
	if apiErr.Code == 404 || apiErr.Status == "NOT_FOUND" {
		return a2a.NewCodedError("channel_not_found", a2a.NewNonRetryableError(err))
	}
	return a2a.NewCodedError("transient", err)
}

// isRateLimitMessage reports whether a free-text error string signals a Google
// quota / rate-limit condition.
func isRateLimitMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "ratelimitexceeded") ||
		strings.Contains(lower, "rate limit exceeded") ||
		strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(msg, "429")
}

func (h *Handler) getClient(ctx context.Context, req a2a.ToolRequest) (GBPClient, string, error) {
	info, err := agentbase.FetchToken(ctx, h.tokens, req.BusinessID, a2a.AgentGoogleBusiness, "", req.Tool)
	if err != nil {
		return nil, "", err
	}
	client := h.clientFactory(info.AccessToken)
	return client, info.ExternalID, nil
}

func (h *Handler) getReviews(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	limit, err := a2a.GetIntParam(req.Args, "limit", gbp.DefaultReviewLimit)
	if err != nil {
		slog.Warn("google_business agent: invalid limit param, using default", "error", err)
		limit = gbp.DefaultReviewLimit
	}
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

	return a2a.OK(req, map[string]any{
		"reviews":        reviews,
		"count":          len(reviews),
		"average_rating": resp.AverageRating,
		"total_count":    resp.TotalReviewCount,
		"has_more":       len(reviews) < resp.TotalReviewCount,
	}), nil
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

	return a2a.OK(req, map[string]any{
		"status":     "replied",
		"reply_text": reply.Comment,
		"updated_at": reply.UpdateTime,
	}), nil
}
