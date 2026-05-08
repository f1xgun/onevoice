package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/yandexcookies"
)

// yandexProbeRequest is the JSON body for both probe and connect.
type yandexProbeRequest struct {
	Cookies string `json:"cookies"`
}

// yandexProbeResponse is the result of a probe attempt.
//
// Field semantics:
//   - Ok: input parsed successfully (does NOT imply session validity).
//   - SessionValid: tri-state via pointer. true → live HTTP probe confirmed
//     login; false → probe redirected to login or returned 401/403; nil →
//     probe failed (network/anti-bot) and we can't determine — accept and
//     let the agent's canary decide on first real call.
//   - Username: best-effort display name pulled from passport.yandex.ru/profile
//     when SessionValid is true. Empty otherwise.
//   - Warnings: missing-but-recommended cookies (sessionid2, yandex_login).
type yandexProbeResponse struct {
	Ok           bool     `json:"ok"`
	Format       string   `json:"format,omitempty"`
	SessionValid *bool    `json:"session_valid,omitempty"`
	Username     string   `json:"username,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// ProbeYandexBusiness validates pasted Yandex cookies without persisting
// anything. Used by the connect modal to give live ✅/❌ feedback as the
// user pastes. Always returns 200 (the "ok" field carries the verdict);
// HTTP errors here would be misread by the UI as network failures.
func (h *OAuthHandler) ProbeYandexBusiness(w http.ResponseWriter, r *http.Request) {
	if _, err := middleware.GetUserID(r.Context()); err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req yandexProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, yandexProbeResponse{Ok: false, Error: "Некорректное тело запроса"})
		return
	}

	parsed, err := yandexcookies.Parse(req.Cookies)
	if err != nil {
		writeJSON(w, http.StatusOK, yandexProbeResponse{Ok: false, Error: err.Error()})
		return
	}

	resp := yandexProbeResponse{
		Ok:       true,
		Format:   parsed.Format,
		Warnings: cookieWarnings(parsed.Cookies),
	}

	// Best-effort live probe. We never block on this; a 2s timeout means
	// the worst case for UX is "format OK, can't verify" which is already
	// better than today's "paste and pray".
	probeCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	valid, username, probeErr := h.probeYandexSession(probeCtx, parsed.Cookies)
	if probeErr != nil {
		slog.Info("yandex session probe inconclusive", "error", probeErr)
		// Leave SessionValid as nil; UI will render "Не удалось проверить".
	} else {
		resp.SessionValid = &valid
		if valid {
			resp.Username = username
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ConnectYandexBusiness persists pasted Yandex cookies as a new active
// integration. Mirrors ConnectTelegram / VK community connect.
func (h *OAuthHandler) ConnectYandexBusiness(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req yandexProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	parsed, err := yandexcookies.Parse(req.Cookies)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get business for Yandex connect", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Resolve the user's Sprav permalink (numeric org id) so the agent can
	// build correct edit URLs like yandex.ru/sprav/<permalink>/p/edit.
	// Without this every RPA tool lands on the marketing landing of
	// business.yandex.ru and silently scrapes nothing. Best-effort: if the
	// lookup fails we still create the integration with externalID="default"
	// so the user can connect even when Yandex ratelimits us.
	permalinkCtx, permalinkCancel := context.WithTimeout(r.Context(), 5*time.Second)
	permalink, lookupErr := h.fetchYandexPermalink(permalinkCtx, parsed.Cookies)
	permalinkCancel()
	externalID := "default"
	if lookupErr == nil && permalink != "" {
		externalID = permalink
	} else if lookupErr != nil {
		slog.Info("yandex connect: permalink lookup failed; falling back to placeholder",
			"error", lookupErr)
	}

	// The integration's friendly name (the actual business name from the
	// Sprav profile) is resolved lazily via POST .../refresh-name which
	// dispatches the agent's RPA get_info tool. Doing it inline here would
	// block connect for 30–60s of Playwright work.
	metadata := map[string]any{
		"input_format": parsed.Format,
		"connected_at": time.Now().UTC().Format(time.RFC3339),
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  business.ID,
		Platform:    "yandex_business",
		ExternalID:  externalID,
		AccessToken: parsed.JSON(),
		Metadata:    metadata,
	})
	if err != nil {
		slog.Error("failed to connect Yandex.Business integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	writeJSON(w, http.StatusCreated, integration)
}

// yandexRefreshTimeout caps the agent's get_info RPA. The Playwright
// scrape — go to /sprav/<permalink>/p/edit, wait for the form, read the
// name input — typically completes in 15–25s; we allow 60s for retries.
const yandexRefreshTimeout = 60 * time.Second

// RefreshYandexBusinessName backfills metadata.business_name on a Yandex
// integration by dispatching the agent's RPA get_info tool over NATS.
// Synchronous: the user clicks "refresh" / the frontend lazily fires this
// once on /integrations load, and we hold the request open until the
// agent replies. With a single integration this is fine; we don't expect
// a stampede.
//
// Returns 200 + the (possibly-updated) integration. Lookup failures are
// non-fatal — we return the original row with a warning logged so the
// frontend's lazy backfill loop doesn't surface transient anti-bot blips
// as errors.
func (h *OAuthHandler) RefreshYandexBusinessName(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.taskPublisher == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "agent task publisher not configured")
		return
	}

	idStr := chi.URLParam(r, "id")
	integrationID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid integration id")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	integrations, err := h.integrationService.ListByBusinessAndPlatform(r.Context(), business.ID, "yandex_business")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	var target *domain.Integration
	for i := range integrations {
		if integrations[i].ID == integrationID {
			target = &integrations[i]
			break
		}
	}
	if target == nil {
		writeJSONError(w, http.StatusNotFound, "integration not found")
		return
	}

	// Detach the agent call + metadata write from the request context.
	// The Playwright RPA takes 30–60s; if the user navigates away or
	// React StrictMode remounts the calling component, axios aborts the
	// HTTP request and r.Context() gets canceled, killing the in-flight
	// NATS request. With the detached context the work completes anyway
	// and the next /integrations load picks up the resolved name.
	bgCtx, bgCancel := context.WithTimeout(context.Background(), yandexRefreshTimeout+15*time.Second)
	go func() {
		defer bgCancel()

		// Heal external_id if it's still the legacy "default" placeholder.
		// Without a real Sprav permalink the agent lands on the marketing
		// landing page instead of the org's edit form, so every selector
		// fails silently and the result map stays empty. Resolve the
		// permalink directly from Yandex's campaign-list API and write it
		// back to the integration before dispatching the agent.
		if target.ExternalID == "default" {
			tokResp, tokErr := h.integrationService.GetDecryptedToken(bgCtx, business.ID, "yandex_business", target.ExternalID)
			if tokErr != nil {
				slog.Info("yandex name refresh: cannot decrypt cookies for permalink heal",
					"integration_id", integrationID, "error", tokErr)
				return
			}
			cookies, parseErr := yandexcookies.Parse(tokResp.AccessToken)
			if parseErr != nil {
				slog.Info("yandex name refresh: stored cookies failed to parse",
					"integration_id", integrationID, "error", parseErr)
				return
			}
			permalink, plErr := h.fetchYandexPermalink(bgCtx, cookies.Cookies)
			if plErr != nil || permalink == "" {
				slog.Info("yandex name refresh: permalink lookup failed during heal",
					"integration_id", integrationID, "error", plErr, "permalink", permalink)
				return
			}
			if updateErr := h.integrationService.UpdateExternalID(bgCtx, integrationID, permalink); updateErr != nil {
				slog.Error("yandex name refresh: failed to persist healed external_id",
					"integration_id", integrationID, "error", updateErr)
				return
			}
			target.ExternalID = permalink
			slog.Info("yandex name refresh: healed external_id",
				"integration_id", integrationID, "permalink", permalink)
		}

		req := a2a.ToolRequest{
			TaskID:     uuid.NewString(),
			Tool:       "yandex_business__get_info",
			Args:       map[string]any{},
			BusinessID: business.ID.String(),
		}
		resp, callErr := h.taskPublisher.RequestTool(bgCtx, a2a.Subject(a2a.AgentYandexBusiness), req, yandexRefreshTimeout)
		if callErr != nil {
			slog.Info("yandex name refresh: agent call failed", "integration_id", integrationID, "error", callErr)
			return
		}
		if resp == nil || resp.Error != "" {
			errMsg := ""
			if resp != nil {
				errMsg = resp.Error
			}
			slog.Info("yandex name refresh: agent returned error", "integration_id", integrationID, "error", errMsg)
			return
		}
		name, _ := resp.Result["name"].(string)
		name = strings.TrimSpace(name)
		resultKeys := make([]string, 0, len(resp.Result))
		for k := range resp.Result {
			resultKeys = append(resultKeys, k)
		}
		slog.Info("yandex name refresh: agent result",
			"integration_id", integrationID,
			"name", name,
			"result_keys", resultKeys,
			"external_id", target.ExternalID)
		if name == "" {
			return
		}
		metadata := map[string]any{}
		for k, v := range target.Metadata {
			metadata[k] = v
		}
		metadata["business_name"] = name
		if updateErr := h.integrationService.UpdateMetadata(bgCtx, integrationID, metadata); updateErr != nil {
			slog.Error("yandex name refresh: failed to persist metadata", "error", updateErr)
			return
		}
		slog.Info("yandex name refresh: persisted", "integration_id", integrationID, "name", name)
	}()

	// Tell the caller "in progress" — they should refetch /integrations
	// after a short delay to pick up the resolved name.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"refresh_started"}`))
}

// cookieWarnings flags missing-but-recommended cookies. Session_id alone
// authenticates most Yandex.Business reads, but writes (reply review,
// upload photo) need sessionid2 and Yandex's anti-CSRF flow expects the
// `yandexuid` / `yandex_login` pair to be present.
func cookieWarnings(cookies []yandexcookies.Cookie) []string {
	have := map[string]bool{}
	for _, c := range cookies {
		have[strings.ToLower(c.Name)] = true
	}
	var warnings []string
	if !have["sessionid2"] {
		warnings = append(warnings, "Не найден sessionid2 — может потребоваться для записи (ответы на отзывы, загрузка фото)")
	}
	if !have["yandex_login"] {
		warnings = append(warnings, "Не найден yandex_login — рекомендуется добавить для стабильной авторизации")
	}
	return warnings
}

// probeYandexSession determines whether the supplied cookies represent a
// live Yandex session. Mirrors the agent's Playwright canary by hitting
// business.yandex.ru and watching for the passport.yandex.ru redirect.
//
// Username is sourced from the `yandex_login` cookie value when present —
// no extra request, no HTML scraping, immune to anti-bot. The Yandex SPA
// returns a JS shell rather than embedded user JSON, so HTML parsing is
// unreliable; the cookie path is.
//
// Signal:
//   - 200 OK on business.yandex.ru → session valid.
//   - 302/303 to passport.yandex.ru* → not logged in.
//   - Anything else → return error so caller treats verdict as "unknown".
func (h *OAuthHandler) probeYandexSession(ctx context.Context, cookies []yandexcookies.Cookie) (valid bool, username string, err error) {
	probeURL := h.yandexProbeURL()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, http.NoBody)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Cookie", buildCookieHeader(cookies))
	// Realistic UA reduces the chance of being served a captcha-gate page.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	req.Header.Set("Accept-Language", "ru,en;q=0.5")

	// Don't follow redirects — we want to see the 302 to passport itself.
	client := *h.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 {
		client.Timeout = 3 * time.Second
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc := resp.Header.Get("Location")
		if loc == "" {
			return false, "", errors.New("redirect with no Location")
		}
		u, parseErr := url.Parse(loc)
		if parseErr == nil && strings.Contains(u.Host, "passport.yandex") {
			return false, "", nil
		}
		// Other redirect (rare; probably internal): treat as live session
		// since we weren't bounced to login.
		return true, "", nil

	case resp.StatusCode == http.StatusOK:
		return true, "", nil

	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return false, "", nil

	default:
		return false, "", errors.New("probe HTTP " + http.StatusText(resp.StatusCode))
	}
}

// buildCookieHeader joins parsed cookies into a single Cookie request header.
func buildCookieHeader(cookies []yandexcookies.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// csrfTokenRegex pulls Yandex's anti-CSRF token out of the dashboard HTML.
// The token shows up either as a JSON property in the inline initial-state
// blob (`"csrfToken":"<hex>:<unix_ts>"`) or in a meta tag
// (`<meta name="csrf-token" content="...">`). We try the JSON form first
// because it's stable across the priority/business pages we hit.
var csrfTokenRegex = regexp.MustCompile(`"csrfToken"\s*:\s*"([^"]+)"`)

// fetchYandexPermalink resolves the user's first Yandex.Business org
// permalink (the numeric Sprav id, e.g. 166299713814) using their pasted
// session cookies. Two-step:
//
//  1. GET https://yandex.ru/business/ to seed an authenticated session and
//     scrape a fresh csrfToken from the dashboard HTML.
//  2. GET https://yandex.ru/business/priority/api/campaign-list/get with
//     that csrfToken — returns JSON with data.result[].companyDescription.permalink.
//
// Returns "" with no error when the user has no orgs registered. Returns
// an error for transport / auth / CSRF failures so the caller can decide
// whether to fall back to a placeholder externalID.
func (h *OAuthHandler) fetchYandexPermalink(ctx context.Context, cookies []yandexcookies.Cookie) (string, error) {
	const (
		dashboardURL    = "https://yandex.ru/business/"
		campaignListURL = "https://yandex.ru/business/priority/api/campaign-list/get"
	)

	cookieHeader := buildCookieHeader(cookies)
	ua := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	// Step 1 — fetch dashboard HTML for csrf token.
	dashReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardURL, http.NoBody)
	if err != nil {
		return "", err
	}
	dashReq.Header.Set("Cookie", cookieHeader)
	dashReq.Header.Set("User-Agent", ua)
	dashReq.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	dashReq.Header.Set("Accept-Language", "ru,en;q=0.5")

	dashResp, err := h.httpClient.Do(dashReq)
	if err != nil {
		return "", fmt.Errorf("dashboard fetch: %w", err)
	}
	defer func() { _ = dashResp.Body.Close() }()
	if dashResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dashboard HTTP %d", dashResp.StatusCode)
	}
	dashBody, _ := io.ReadAll(io.LimitReader(dashResp.Body, 1<<20)) // 1MB
	csrfMatch := csrfTokenRegex.FindSubmatch(dashBody)
	if len(csrfMatch) < 2 {
		return "", errors.New("csrfToken not found in dashboard HTML")
	}
	csrfToken := string(csrfMatch[1])

	// Step 2 — campaign list. sessionId is a client-side nonce; Yandex
	// validates only the csrfToken cryptographically. Use a timestamp-based
	// value matching Yandex's `<unix_ms>_<6-digit>` pattern to be safe.
	sessionID := fmt.Sprintf("%d_%d", time.Now().UnixMilli(), time.Now().UnixNano()%1_000_000)
	q := url.Values{}
	q.Set("csrfToken", csrfToken)
	q.Set("sessionId", sessionID)
	q.Set("limit", "20")
	q.Set("offset", "0")

	listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, campaignListURL+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return "", err
	}
	listReq.Header.Set("Cookie", cookieHeader)
	listReq.Header.Set("User-Agent", ua)
	listReq.Header.Set("Accept", "application/json")
	listReq.Header.Set("Accept-Language", "ru,en;q=0.5")
	listReq.Header.Set("Referer", dashboardURL)

	listResp, err := h.httpClient.Do(listReq)
	if err != nil {
		return "", fmt.Errorf("campaign-list fetch: %w", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("campaign-list HTTP %d", listResp.StatusCode)
	}
	listBody, _ := io.ReadAll(io.LimitReader(listResp.Body, 1<<20))

	var parsed struct {
		Data struct {
			Result []struct {
				CompanyDescription struct {
					// Yandex returns this as a JSON number, not a string.
					// Decode into json.Number to preserve precision then
					// stringify — int64 is fine for current ids but
					// future-proofs against larger values.
					Permalink json.Number `json:"permalink"`
				} `json:"companyDescription"`
			} `json:"result"`
		} `json:"data"`
	}
	dec := json.NewDecoder(strings.NewReader(string(listBody)))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return "", fmt.Errorf("parse campaign-list: %w", err)
	}
	if len(parsed.Data.Result) == 0 {
		return "", nil // legitimately no orgs registered
	}
	permalinkStr := strings.TrimSpace(parsed.Data.Result[0].CompanyDescription.Permalink.String())
	if permalinkStr == "" {
		return "", errors.New("permalink missing in campaign-list response")
	}
	// Sanity check: must be a positive integer.
	if _, perr := strconv.ParseUint(permalinkStr, 10, 64); perr != nil {
		return "", fmt.Errorf("permalink not numeric: %q", permalinkStr)
	}
	return permalinkStr, nil
}

// yandexProbeURL returns the live-probe endpoint, honoring an optional
// test override on OAuthConfig. Production target mirrors the agent's
// Playwright canary (business.yandex.ru — 302's unauthenticated visitors
// to passport.yandex.ru/auth, returns 200 for live sessions).
func (h *OAuthHandler) yandexProbeURL() string {
	if h.cfg.yandexProbeBaseURL != "" {
		return h.cfg.yandexProbeBaseURL
	}
	return "https://business.yandex.ru/"
}
