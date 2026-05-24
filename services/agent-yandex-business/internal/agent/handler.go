package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/yandex"
)

// TokenInfo aliases agentbase.TokenInfo so existing test mocks compile.
// ExternalID carries the Yandex Sprav permalink.
type TokenInfo = agentbase.TokenInfo

// TokenFetcher aliases agentbase.TokenResolver — kept for test
// compatibility (import-path/wiring-only changes in handler_test.go).
type TokenFetcher = agentbase.TokenResolver

// YandexBrowser abstracts Playwright browser operations for testability.
type YandexBrowser interface {
	GetInfo(ctx context.Context) (map[string]interface{}, error)
	UpdateHours(ctx context.Context, hoursJSON string) error
	UpdateInfo(ctx context.Context, info map[string]string) error
	GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error)
	ReplyReview(ctx context.Context, reviewID, text string) error
	CreatePost(ctx context.Context, text string) error
	UploadPhoto(ctx context.Context, photoURL, category string) error
	// ListCompanies hits yandex.ru/sprav/companies and returns the user's
	// organizations (numeric Sprav permalink + display name). Used by the
	// API service to resolve external_id + business_name on connect /
	// refresh-name. Permalink-independent — does not require the
	// BusinessBrowser to have a known permalink.
	ListCompanies(ctx context.Context) ([]map[string]interface{}, error)
}

// BrowserPool abstracts the shared Playwright browser pool.
type BrowserPool interface {
	ForBusiness(businessID, cookiesJSON, permalink string) YandexBrowser
}

// Handler is the Yandex.Business RPA agent's per-request processor. Its
// Handle method satisfies a2a.Exec and is wired into a2a.NewAgent from
// cmd/main.go. The dispatch chain (dispatcher fallback + per-tool routing +
// "unknown tool" error) lives in agentbase.NewRouter.
type Handler struct {
	tokens TokenFetcher
	pool   BrowserPool
	exec   agentbase.ToolExec
}

// NewHandler creates a Handler with the given TokenFetcher, BrowserPool, and
// agentbase.Dispatcher. The dispatcher owns the HITL dedupe gate and error
// classification (see pkg/agentbase). A nil dispatcher disables HITL — on
// that path the router applies ClassifyYandexError as the fallback classifier.
//
// The HITL dedupe gate runs BEFORE Playwright acquires a browser page — page
// acquisition is expensive, so dedupe avoids spinning up a Chromium tab for a
// replay. The `withRetry + withPage` pattern inside yandex/pool.go is
// unchanged by this wiring.
func NewHandler(tokens TokenFetcher, pool BrowserPool, dispatcher agentbase.Dispatcher) *Handler {
	h := &Handler{tokens: tokens, pool: pool}
	h.exec = agentbase.NewRouter(h.routes(), dispatcher, agentbase.FuncClassifier(ClassifyYandexError))
	return h
}

// Handle is the a2a.Exec entry point — a thin shim over the router built in
// NewHandler.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	return h.exec(ctx, req)
}

// routes binds the Yandex.Business tool catalog to the Handler's per-tool
// methods.
func (h *Handler) routes() map[string]agentbase.ToolExec {
	return map[string]agentbase.ToolExec{
		tools.YandexBusinessGetInfo:       h.getInfo,
		tools.YandexBusinessUpdateHours:   h.updateHours,
		tools.YandexBusinessUpdateInfo:    h.updateInfo,
		tools.YandexBusinessGetReviews:    h.getReviews,
		tools.YandexBusinessReplyReview:   h.replyReview,
		tools.YandexBusinessCreatePost:    h.createPost,
		tools.YandexBusinessUploadPhoto:   h.uploadPhoto,
		tools.YandexBusinessListCompanies: h.listCompanies,
	}
}

// ClassifyYandexError is the exported entry point used by cmd/main.go to wire
// the dispatcher's classifier via agentbase.FuncClassifier. Body unchanged.
func ClassifyYandexError(err error) error {
	return classifyYandexError(err)
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
	info, err := h.tokens.GetToken(ctx, req.BusinessID, a2a.AgentYandexBusiness, "")
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

func (h *Handler) listCompanies(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	// list_companies is the only tool that can be invoked BEFORE an
	// integration row exists (during the connect-flow company picker).
	// In that case the API service passes cookies inline via req.Args
	// instead of relying on tokenclient → DB → decrypt.
	if cookiesJSON, ok := req.Args["cookies"].(string); ok && cookiesJSON != "" {
		browser := h.pool.ForBusiness(req.BusinessID, cookiesJSON, "")
		companies, listErr := browser.ListCompanies(ctx)
		if listErr != nil {
			return nil, fmt.Errorf("yandex: list companies: %w", classifyYandexError(listErr))
		}
		return &a2a.ToolResponse{
			TaskID:  req.TaskID,
			Success: true,
			Result:  map[string]interface{}{"companies": companies},
		}, nil
	}
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	companies, err := browser.ListCompanies(ctx)
	if err != nil {
		return nil, fmt.Errorf("yandex: list companies: %w", classifyYandexError(err))
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"companies": companies},
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
