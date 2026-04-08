package agent

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// TokenInfo holds the resolved tokens for an integration.
type TokenInfo struct {
	AccessToken string
	ExternalID  string
}

// TokenFetcher abstracts token retrieval for testability.
type TokenFetcher interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

// GBPClient abstracts Google Business Profile API operations for testability.
// Methods will be added in Phase 11 (reviews), Phase 12 (business info), etc.
type GBPClient interface {
	// Phase 10: no methods yet - agent scaffold only.
}

// GBPClientFactory creates a GBP client from an access token.
type GBPClientFactory func(accessToken string) GBPClient

// Handler implements a2a.Handler for the Google Business agent.
type Handler struct {
	tokens        TokenFetcher
	clientFactory GBPClientFactory
}

// NewHandler creates a Handler with per-request token fetching.
func NewHandler(tokens TokenFetcher, factory GBPClientFactory) *Handler {
	return &Handler{tokens: tokens, clientFactory: factory}
}

// Handle routes the ToolRequest to the appropriate GBP API operation.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	// Phase 10: No functional tools yet.
	// Phase 11 will add: google_business__get_reviews, google_business__reply_review, etc.
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}
