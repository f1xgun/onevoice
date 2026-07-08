package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/domain"
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
	// VerifyAccess confirms the bound session can reach and edit the org at the
	// browser's permalink, returning true when the edit form mounts.
	VerifyAccess(ctx context.Context) (bool, error)
}

// BrowserPool abstracts the shared Playwright browser pool.
type BrowserPool interface {
	// ForBusiness binds a per-business credential (legacy cookie-paste / OAuth).
	ForBusiness(businessID, cookiesJSON, permalink string) YandexBrowser
	// ForSharedBusiness binds the shared representative session to a business's
	// permalink (delegated-representative access). sharedCookies is the shared
	// singleton session; permalink is the ONLY tenant scope.
	ForSharedBusiness(businessID, sharedCookies, permalink string) YandexBrowser
}

// SharedSession abstracts resolution of the shared representative session.
// Satisfied by agentbase.SharedSessionResolver. Nil disables the delegated
// path — getBrowser then returns a clear "not configured" error for any
// integration that resolves with an empty per-business credential.
type SharedSession interface {
	GetSharedSession(ctx context.Context, platform, reason string) (string, error)
}

// Handler is the Yandex.Business RPA agent's per-request processor. Its
// Handle method satisfies a2a.Exec and is wired into a2a.NewAgent from
// cmd/main.go. The dispatch chain (dispatcher fallback + per-tool routing +
// "unknown tool" error) lives in agentbase.NewRouter.
type Handler struct {
	tokens TokenFetcher
	pool   BrowserPool
	shared SharedSession
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

// WithSharedSession injects the shared representative session resolver used by
// the delegated-representative access path. Not constructor-injected so
// existing call sites and tests stay untouched; nil (the default) keeps the
// delegated path fail-closed — an integration with no per-business credential
// returns a clear "delegated access not configured" error. Returns the handler
// for chaining.
func (h *Handler) WithSharedSession(s SharedSession) *Handler {
	h.shared = s
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
		tools.YandexBusinessVerifyAccess:  h.verifyAccess,
	}
}

// ClassifyYandexError is the exported entry point used by cmd/main.go to wire
// the dispatcher's classifier via agentbase.FuncClassifier. Body unchanged.
func ClassifyYandexError(err error) error {
	return classifyYandexError(err)
}

// classifyYandexError stamps permanent Yandex RPA errors with a typed Code
// and composes with NonRetryableError. Session-expired and Passport redirect
// errors map to integration_token_invalid; CAPTCHA maps to rate_limit_exceeded;
// review-not-found and reply-form-unavailable map to transient (permanent at
// the RPA layer but not actionable by a reconnect).
func classifyYandexError(err error) error {
	if err == nil {
		return nil
	}
	if a2a.CodeOf(err) != "" {
		return err
	}
	if errors.Is(err, yandex.ErrSessionExpired) {
		return a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(err))
	}
	msg := err.Error()
	if strings.Contains(msg, "session expired") || strings.Contains(msg, "login redirect") || strings.Contains(msg, "passport.yandex") {
		return a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(err))
	}
	if strings.Contains(msg, "captcha") || strings.Contains(msg, "CAPTCHA") {
		return a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(fmt.Errorf("yandex captcha detected: %w", err)))
	}
	if strings.Contains(msg, "review not found") {
		return a2a.NewCodedError("transient", a2a.NewNonRetryableError(err))
	}
	if strings.Contains(msg, "reply form unavailable") || strings.Contains(msg, "reply button not found") {
		return a2a.NewCodedError("transient", a2a.NewNonRetryableError(err))
	}
	return a2a.NewCodedError("transient", err)
}

// ErrDelegatedNotConfigured is returned when a business resolves to a delegated
// integration (no per-business credential) but the shared representative
// session plane is not provisioned (no SharedSession resolver, or its sentinel
// is unset). It carries a coded, non-retryable classification so the caller
// surfaces a clear "delegated access not configured" instead of a transient
// retry.
var ErrDelegatedNotConfigured = errors.New("delegated access not configured")

// getBrowser resolves the credential for req.BusinessID and returns a browser
// bound to it. It branches on the resolved credential:
//
//   - AccessToken != "" → per-business credential (cookie-paste / OAuth legacy
//     path) → ForBusiness, entirely UNCHANGED.
//   - AccessToken == "" → delegated-representative access: the integration row
//     carries only a permalink (ExternalID) and no credential, so the SHARED
//     representative session is resolved and bound via ForSharedBusiness. The
//     permalink comes exclusively from the resolved integration row
//     (info.ExternalID), never from LLM/task args — the multi-tenant isolation
//     invariant.
//
// When the delegated branch is taken but no shared session is provisioned it
// fails closed with ErrDelegatedNotConfigured.
func (h *Handler) getBrowser(ctx context.Context, req a2a.ToolRequest) (YandexBrowser, error) {
	return h.getBrowserForExternalID(ctx, req, "")
}

// getBrowserForExternalID resolves the credential using resolveExternalID as the
// token-resolution key. An empty resolveExternalID means "first active
// integration for the platform" (the default every non-verify tool uses). A
// non-empty value is used ONLY to pick a specific delegated row when a business
// has more than one delegated org — it cannot broaden isolation because the
// token client resolves an EXACT external_id match against a real integration
// row owned by req.BusinessID, and the bound permalink is always the resolved
// row's authoritative info.ExternalID, never the raw hint.
func (h *Handler) getBrowserForExternalID(ctx context.Context, req a2a.ToolRequest, resolveExternalID string) (YandexBrowser, error) {
	info, err := agentbase.FetchToken(ctx, h.tokens, req.BusinessID, a2a.AgentYandexBusiness, resolveExternalID, req.Tool)
	if err != nil {
		return nil, err
	}
	if info.AccessToken != "" {
		return h.pool.ForBusiness(req.BusinessID, info.AccessToken, info.ExternalID), nil
	}
	return h.sharedBrowser(ctx, req, info.ExternalID)
}

// sharedBrowser binds the shared representative session to the given permalink.
// permalink MUST be the integration row's external_id (resolved server-side),
// not a task arg. Fails closed with ErrDelegatedNotConfigured when the shared
// plane is unprovisioned.
func (h *Handler) sharedBrowser(ctx context.Context, req a2a.ToolRequest, permalink string) (YandexBrowser, error) {
	if h.shared == nil {
		return nil, a2a.NewCodedError("integration_not_configured", a2a.NewNonRetryableError(ErrDelegatedNotConfigured))
	}
	sharedCookies, err := h.shared.GetSharedSession(ctx, a2a.AgentYandexBusiness, req.Tool)
	if err != nil {
		if errors.Is(err, agentbase.ErrSharedSessionNotConfigured) {
			return nil, a2a.NewCodedError("integration_not_configured", a2a.NewNonRetryableError(ErrDelegatedNotConfigured))
		}
		return nil, err
	}
	return h.pool.ForSharedBusiness(req.BusinessID, sharedCookies, permalink), nil
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
	return a2a.OK(req, info), nil
}

func (h *Handler) listCompanies(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	if cookiesJSON, ok := req.Args["cookies"].(string); ok && cookiesJSON != "" {
		browser := h.pool.ForBusiness(req.BusinessID, cookiesJSON, "")
		companies, listErr := browser.ListCompanies(ctx)
		if listErr != nil {
			return nil, fmt.Errorf("yandex: list companies: %w", classifyYandexError(listErr))
		}
		return a2a.OK(req, map[string]any{"companies": companies}), nil
	}
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	companies, err := browser.ListCompanies(ctx)
	if err != nil {
		return nil, fmt.Errorf("yandex: list companies: %w", classifyYandexError(err))
	}
	return a2a.OK(req, map[string]any{"companies": companies}), nil
}

// verifyAccess confirms the shared representative session can reach the org
// bound to this business's integration row. The permalink is resolved from the
// integration row inside getBrowser (never from task args), so the isolation
// assertion on navigation is sound. It always uses the delegated shared path —
// the API dispatches it only for delegated integrations — and getBrowser fails
// closed with the delegated-not-configured error when the shared plane is
// unprovisioned.
func (h *Handler) verifyAccess(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	permalink, _ := req.Args["permalink"].(string)
	browser, err := h.getBrowserForExternalID(ctx, req, permalink)
	if err != nil {
		return nil, err
	}

	detected, err := browser.VerifyAccess(ctx)
	if err != nil {
		return nil, fmt.Errorf("yandex: verify access: %w", classifyYandexError(err))
	}
	return a2a.OK(req, map[string]any{"access_verified": detected}), nil
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
	return a2a.OK(req, map[string]any{"status": "updated", "note": "changes pending Yandex moderation"}), nil
}

func (h *Handler) updateInfo(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	info := make(map[string]string)
	for _, key := range []string{"phone", "description"} {
		if v, ok := req.Args[key].(string); ok {
			info[key] = v
		}
	}
	if err := browser.UpdateInfo(ctx, info); err != nil {
		return nil, fmt.Errorf("yandex: update info: %w", classifyYandexError(err))
	}
	return a2a.OK(req, map[string]any{"status": "updated", "note": "changes pending Yandex moderation"}), nil
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
	return a2a.OK(req, map[string]any{"status": "published"}), nil
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
	return a2a.OK(req, map[string]any{"status": "uploaded", "note": "photo pending Yandex moderation"}), nil
}

func (h *Handler) getReviews(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}

	limit, err := a2a.GetIntParam(req.Args, "limit", domain.YandexBusinessReviewLimitDefault)
	if err != nil {
		slog.Warn("yandex agent: invalid limit param, using default", "error", err)
		limit = domain.YandexBusinessReviewLimitDefault
	}
	if limit == 0 {
		limit = domain.YandexBusinessReviewLimitDefault
	}

	reviews, err := browser.GetReviews(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("yandex: get reviews: %w", classifyYandexError(err))
	}
	return a2a.OK(req, map[string]any{"reviews": reviews, "count": len(reviews)}), nil
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
	return a2a.OK(req, map[string]any{"status": "replied"}), nil
}
