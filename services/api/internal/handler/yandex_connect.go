package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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

	// Connect stores the integration with external_id="default" as a
	// placeholder. The Sprav permalink and human business name are
	// resolved out-of-band via POST .../refresh-name, which dispatches the
	// agent's yandex_business__list_companies RPA tool. We can't resolve
	// inline because /sprav/companies is a SPA — server-rendered HTML is
	// empty, only Playwright sees the hydrated org list.
	metadata := map[string]any{
		"input_format": parsed.Format,
		"connected_at": time.Now().UTC().Format(time.RFC3339),
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  business.ID,
		Platform:    "yandex_business",
		ExternalID:  "default",
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

// yandexListCompaniesTimeout caps the agent's list_companies RPA. The
// page is a Yandex SPA — Playwright must wait for client-side hydration
// to render the org list. 30s cap with one retry inside withRetry.
const yandexListCompaniesTimeout = 60 * time.Second

// RefreshYandexBusinessName backfills metadata.business_name and (when
// missing) the Sprav permalink on an existing Yandex integration. The
// /sprav/companies page is a SPA — server-rendered HTML is empty — so
// we dispatch the agent's yandex_business__list_companies tool which
// drives a real Playwright browser. Async fire-and-forget: returns 202
// immediately, the frontend polls /integrations to pick up the new
// business_name once persisted.
//
// Lookup failures are non-fatal: logged, integration left untouched.
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

	// Detach the agent dispatch from the request context — Playwright RPA
	// takes 20–40s, easily longer than axios's idle timeout, and the work
	// must complete even if the user navigates away mid-flight.
	bgCtx, bgCancel := context.WithTimeout(context.Background(), yandexListCompaniesTimeout+15*time.Second)
	go h.runYandexListCompaniesRefresh(bgCtx, bgCancel, integrationID, *target, business.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"refresh_started"}`))
}

// runYandexListCompaniesRefresh performs the actual list_companies RPA
// dispatch + metadata writes in a detached goroutine. Heals legacy
// external_id="default" rows along the way: the Sprav permalink we
// extract from the agent response replaces the placeholder and lets
// every subsequent agent call (reviews, info, posts) build correct
// /sprav/<permalink>/p/edit URLs.
func (h *OAuthHandler) runYandexListCompaniesRefresh(
	ctx context.Context,
	cancel context.CancelFunc,
	integrationID uuid.UUID,
	target domain.Integration,
	businessID uuid.UUID,
) {
	defer cancel()

	req := a2a.ToolRequest{
		TaskID:     uuid.NewString(),
		Tool:       "yandex_business__list_companies",
		Args:       map[string]any{},
		BusinessID: businessID.String(),
	}
	resp, callErr := h.taskPublisher.RequestTool(ctx, a2a.Subject(a2a.AgentYandexBusiness), req, yandexListCompaniesTimeout)
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

	// Result shape: {"companies": [{"permalink": "...", "name": "..."}, ...]}
	companiesRaw, _ := resp.Result["companies"].([]any)
	if len(companiesRaw) == 0 {
		slog.Info("yandex name refresh: agent returned no companies", "integration_id", integrationID)
		return
	}
	first, _ := companiesRaw[0].(map[string]any)
	permalink, _ := first["permalink"].(string)
	name, _ := first["name"].(string)
	permalink = strings.TrimSpace(permalink)
	name = strings.TrimSpace(name)

	if target.ExternalID == "default" && permalink != "" {
		if updateErr := h.integrationService.UpdateExternalID(ctx, integrationID, permalink); updateErr != nil {
			slog.Error("yandex name refresh: failed to persist healed external_id",
				"integration_id", integrationID, "error", updateErr)
		} else {
			target.ExternalID = permalink
			slog.Info("yandex name refresh: healed external_id",
				"integration_id", integrationID, "permalink", permalink)
		}
	}

	if name != "" {
		metadata := map[string]any{}
		for k, v := range target.Metadata {
			metadata[k] = v
		}
		metadata["business_name"] = name
		if updateErr := h.integrationService.UpdateMetadata(ctx, integrationID, metadata); updateErr != nil {
			slog.Error("yandex name refresh: failed to persist business_name",
				"integration_id", integrationID, "error", updateErr)
			return
		}
		slog.Info("yandex name refresh: persisted",
			"integration_id", integrationID, "name", name, "external_id", target.ExternalID)
	}
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
