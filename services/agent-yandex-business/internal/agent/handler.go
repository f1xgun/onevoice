package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/yandex"
)

// TokenInfo holds the resolved token and external ID for an integration.
type TokenInfo struct {
	AccessToken string
	ExternalID  string // Yandex Sprav permalink
}

// TokenFetcher retrieves an access token (cookies JSON) for a given business/platform combination.
type TokenFetcher interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

// YandexBrowser abstracts Playwright browser operations for testability.
type YandexBrowser interface {
	GetInfo(ctx context.Context) (map[string]interface{}, error)
	UpdateHours(ctx context.Context, hoursJSON string) error
	UpdateInfo(ctx context.Context, info map[string]string) error
	GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error)
	ReplyReview(ctx context.Context, reviewID, text string) error
	CreatePost(ctx context.Context, text string) error
	UploadPhoto(ctx context.Context, photoURL, category string) error
}

// BrowserPool abstracts the shared Playwright browser pool.
type BrowserPool interface {
	ForBusiness(businessID, cookiesJSON, permalink string) YandexBrowser
}

// Handler implements a2a.Handler for the Yandex.Business RPA agent.
type Handler struct {
	tokens TokenFetcher
	pool   BrowserPool
	dedupe *hitldedupe.DedupeClient // optional; nil skips the HITL dedupe gate
}

// NewHandler creates a Handler with the given TokenFetcher, BrowserPool, and
// optional dedupe client. Passing nil for dedupe disables the HITL dedupe
// gate — used by unit tests and dev-local environments without Redis.
func NewHandler(tokens TokenFetcher, pool BrowserPool, dedupe *hitldedupe.DedupeClient) *Handler {
	return &Handler{tokens: tokens, pool: pool, dedupe: dedupe}
}

// Handle routes ToolRequests to the appropriate Yandex.Business operation.
// The HITL dedupe gate runs BEFORE Playwright acquires a browser page from the
// pool — this matters for the RPA agent because a page acquisition is
// expensive; deduping early avoids spinning up a Chromium tab for a replay.
// The `withRetry + withPage` pattern inside yandex/pool.go remains unchanged.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	if resp, stop := h.dedupeGate(ctx, req); stop {
		return resp, nil
	}

	var (
		resp *a2a.ToolResponse
		err  error
	)
	switch req.Tool {
	case "yandex_business__get_info":
		resp, err = h.getInfo(ctx, req)
	case "yandex_business__update_hours":
		resp, err = h.updateHours(ctx, req)
	case "yandex_business__update_info":
		resp, err = h.updateInfo(ctx, req)
	case "yandex_business__get_reviews":
		resp, err = h.getReviews(ctx, req)
	case "yandex_business__reply_review":
		resp, err = h.replyReview(ctx, req)
	case "yandex_business__create_post":
		resp, err = h.createPost(ctx, req)
	case "yandex_business__upload_photo":
		resp, err = h.uploadPhoto(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}

	h.dedupeStore(ctx, req, resp, err)
	return resp, err
}

// dedupeGate consults the Redis dedupe cache BEFORE tool dispatch when a HITL
// approval is in play. Returns (resp, true) when the caller should stop.
func (h *Handler) dedupeGate(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, bool) {
	if h.dedupe == nil || req.ApprovalID == "" {
		return nil, false
	}
	outcome, cached, err := h.dedupe.Claim(ctx, req.BusinessID, req.ApprovalID)
	if err != nil {
		slog.WarnContext(ctx, "hitl dedupe claim failed; proceeding without dedupe",
			"error", err, "business_id", req.BusinessID, "approval_id", req.ApprovalID)
		return nil, false
	}
	switch outcome {
	case hitldedupe.ClaimOutcomeInFlight:
		return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: already in flight"}, true
	case hitldedupe.ClaimOutcomeDuplicate:
		var cachedResp a2a.ToolResponse
		if uerr := json.Unmarshal([]byte(cached), &cachedResp); uerr != nil {
			slog.WarnContext(ctx, "hitl dedupe cached result malformed; returning generic duplicate",
				"error", uerr)
			return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: cached result unavailable"}, true
		}
		cachedResp.TaskID = req.TaskID
		return &cachedResp, true
	case hitldedupe.ClaimOutcomeClaimed, hitldedupe.ClaimOutcomeSkip:
		// Proceed with execution — no cached response.
	}
	return nil, false
}

// dedupeStore persists a successful ToolResponse under the HITL dedupe key
// so replays see ClaimOutcomeDuplicate. Errors/nil responses are not cached.
func (h *Handler) dedupeStore(ctx context.Context, req a2a.ToolRequest, resp *a2a.ToolResponse, err error) {
	if h.dedupe == nil || req.ApprovalID == "" || err != nil || resp == nil {
		return
	}
	if serr := h.dedupe.Store(ctx, req.BusinessID, req.ApprovalID, resp); serr != nil {
		slog.WarnContext(ctx, "hitl dedupe store failed; result returned but not cached",
			"error", serr, "approval_id", req.ApprovalID)
	}
}

// classifyYandexError wraps permanent Yandex RPA errors as NonRetryableError.
func classifyYandexError(err error) error {
	if err == nil {
		return nil
	}
	// Sentinel check — canary already wrapped in NonRetryableError, propagate as-is
	if errors.Is(err, yandex.ErrSessionExpired) {
		return a2a.NewNonRetryableError(err)
	}
	msg := err.Error()
	// Session expired — login redirect detected
	if strings.Contains(msg, "session expired") || strings.Contains(msg, "login redirect") || strings.Contains(msg, "passport.yandex") {
		return a2a.NewNonRetryableError(err)
	}
	// CAPTCHA — rate-limited, non-retryable
	if strings.Contains(msg, "captcha") || strings.Contains(msg, "CAPTCHA") {
		return a2a.NewNonRetryableError(fmt.Errorf("yandex captcha detected: %w", err))
	}
	// Review not found — no point retrying
	if strings.Contains(msg, "review not found") {
		return a2a.NewNonRetryableError(err)
	}
	// Reply form unavailable (already replied or reviews disabled)
	if strings.Contains(msg, "reply form unavailable") || strings.Contains(msg, "reply button not found") {
		return a2a.NewNonRetryableError(err)
	}
	return err // transient (timeout, network, etc.)
}

func (h *Handler) getBrowser(ctx context.Context, req a2a.ToolRequest) (YandexBrowser, error) {
	info, err := h.tokens.GetToken(ctx, req.BusinessID, "yandex_business", "")
	if err != nil {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("fetch token: %w", err))
	}
	return h.pool.ForBusiness(req.BusinessID, info.AccessToken, info.ExternalID), nil
}

func (h *Handler) getInfo(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	info, err := browser.GetInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("yandex: get info: %w", classifyYandexError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  info,
	}, nil
}

func (h *Handler) updateHours(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	hours, _ := req.Args["hours"].(string)
	if err := browser.UpdateHours(ctx, hours); err != nil {
		return nil, fmt.Errorf("yandex: update hours: %w", classifyYandexError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated", "note": "changes pending Yandex moderation"},
	}, nil
}

func (h *Handler) updateInfo(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	info := make(map[string]string)
	for _, key := range []string{"phone", "website", "description"} {
		if v, ok := req.Args[key].(string); ok {
			info[key] = v
		}
	}
	if err := browser.UpdateInfo(ctx, info); err != nil {
		return nil, fmt.Errorf("yandex: update info: %w", classifyYandexError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated", "note": "changes pending Yandex moderation"},
	}, nil
}

func (h *Handler) createPost(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	text, _ := req.Args["text"].(string)
	if err := browser.CreatePost(ctx, text); err != nil {
		return nil, fmt.Errorf("yandex: create post: %w", classifyYandexError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "published"},
	}, nil
}

func (h *Handler) uploadPhoto(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	photoURL, _ := req.Args["photo_url"].(string)
	category, _ := req.Args["category"].(string)
	if category == "" {
		category = "general"
	}
	if err := browser.UploadPhoto(ctx, photoURL, category); err != nil {
		return nil, fmt.Errorf("yandex: upload photo: %w", classifyYandexError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "uploaded", "note": "photo pending Yandex moderation"},
	}, nil
}

func (h *Handler) getReviews(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	limitF, _ := req.Args["limit"].(float64)
	limit := int(limitF)
	if limit == 0 {
		limit = 20
	}

	reviews, err := browser.GetReviews(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("yandex: get reviews: %w", classifyYandexError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"reviews": reviews, "count": len(reviews)},
	}, nil
}

func (h *Handler) replyReview(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	reviewID, _ := req.Args["review_id"].(string)
	text, _ := req.Args["text"].(string)

	if err := browser.ReplyReview(ctx, reviewID, text); err != nil {
		return nil, fmt.Errorf("yandex: reply review: %w", classifyYandexError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "replied"},
	}, nil
}
