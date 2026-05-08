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

	// Resolve the user's Sprav org info inline. One quick HTTP scrape of
	// yandex.ru/sprav/companies gives us both the numeric permalink (used
	// as external_id so agent edit URLs are correct) and the human
	// business name (stored in metadata.business_name for display).
	// Best-effort: a lookup failure still lets the integration be created
	// with externalID="default", and refresh-name retries later.
	infoCtx, infoCancel := context.WithTimeout(r.Context(), 5*time.Second)
	info, lookupErr := h.fetchYandexBusinessInfo(infoCtx, parsed.Cookies)
	infoCancel()
	externalID := "default"
	if lookupErr == nil && info.Permalink != "" {
		externalID = info.Permalink
	} else if lookupErr != nil {
		slog.Info("yandex connect: business info lookup failed; using placeholder",
			"error", lookupErr)
	}

	metadata := map[string]any{
		"input_format": parsed.Format,
		"connected_at": time.Now().UTC().Format(time.RFC3339),
	}
	if info.Name != "" {
		metadata["business_name"] = info.Name
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

// RefreshYandexBusinessName backfills metadata.business_name and (when
// missing) the Sprav permalink on an existing Yandex integration. Drives
// the same one-shot HTTP scrape of yandex.ru/sprav/companies the connect
// path uses — sub-second, no NATS, no Playwright dependency.
//
// Returns 200 + the (possibly-updated) integration. Lookup failures are
// non-fatal: we return the original row and log so the frontend's lazy
// backfill loop never surfaces transient anti-bot blips as errors.
func (h *OAuthHandler) RefreshYandexBusinessName(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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

	// Decrypt the stored cookies and run the same one-shot HTTP scrape the
	// connect path uses. Resolves both the Sprav permalink (heals legacy
	// external_id="default" rows) and the human business name, in well
	// under a second. Synchronous: no NATS dispatch, no goroutine.
	tokResp, tokErr := h.integrationService.GetDecryptedToken(r.Context(), business.ID, "yandex_business", target.ExternalID)
	if tokErr != nil {
		slog.Info("yandex name refresh: cannot decrypt cookies", "integration_id", integrationID, "error", tokErr)
		writeJSON(w, http.StatusOK, target)
		return
	}
	cookies, parseErr := yandexcookies.Parse(tokResp.AccessToken)
	if parseErr != nil {
		slog.Info("yandex name refresh: stored cookies failed to parse", "integration_id", integrationID, "error", parseErr)
		writeJSON(w, http.StatusOK, target)
		return
	}
	infoCtx, infoCancel := context.WithTimeout(r.Context(), 5*time.Second)
	info, infoErr := h.fetchYandexBusinessInfo(infoCtx, cookies.Cookies)
	infoCancel()
	if infoErr != nil {
		slog.Info("yandex name refresh: business info lookup failed",
			"integration_id", integrationID, "error", infoErr)
		writeJSON(w, http.StatusOK, target)
		return
	}

	// Heal the external_id if it was still the legacy "default" placeholder.
	if target.ExternalID == "default" && info.Permalink != "" {
		if updateErr := h.integrationService.UpdateExternalID(r.Context(), integrationID, info.Permalink); updateErr != nil {
			slog.Error("yandex name refresh: failed to persist healed external_id",
				"integration_id", integrationID, "error", updateErr)
		} else {
			target.ExternalID = info.Permalink
			slog.Info("yandex name refresh: healed external_id",
				"integration_id", integrationID, "permalink", info.Permalink)
		}
	}

	// Persist the resolved business name into metadata.
	if info.Name != "" {
		metadata := map[string]any{}
		for k, v := range target.Metadata {
			metadata[k] = v
		}
		metadata["business_name"] = info.Name
		if updateErr := h.integrationService.UpdateMetadata(r.Context(), integrationID, metadata); updateErr != nil {
			slog.Error("yandex name refresh: failed to persist business_name",
				"integration_id", integrationID, "error", updateErr)
		} else {
			target.Metadata = metadata
			slog.Info("yandex name refresh: persisted",
				"integration_id", integrationID, "name", info.Name, "external_id", target.ExternalID)
		}
	}

	writeJSON(w, http.StatusOK, target)
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

// spravPermalinkRegex pulls a numeric Sprav permalink out of an org-edit
// link on the companies list page. Pattern: `/sprav/<digits>/p/edit...`.
// Anchored on `/p/edit` to avoid catching unrelated /sprav/api/... paths.
var spravPermalinkRegex = regexp.MustCompile(`/sprav/(\d+)/p/edit`)

// spravNameRegex pulls the org display name from the first
// CompanyInfoCard-CompanyName <h4>. Yandex's React markup is stable
// enough on this page that a tag-content regex is safe.
var spravNameRegex = regexp.MustCompile(`<h4[^>]*CompanyInfoCard-CompanyName[^>]*>([^<]+)</h4>`)

// htmlEntityReplacer expands the few HTML entities Yandex emits in this
// stretch of markup. We avoid pulling in a full HTML parser since the
// rest of the file uses lightweight regex-based extraction.
var htmlEntityReplacer = strings.NewReplacer(
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&#39;", "'",
	"&nbsp;", " ",
)

// yandexBusinessInfo is the resolved {Sprav permalink, display name} pair
// returned by fetchYandexBusinessInfo.
type yandexBusinessInfo struct {
	Permalink string
	Name      string
}

// fetchYandexBusinessInfo resolves the user's first Sprav organization
// (numeric permalink + display name) using their pasted session cookies.
// Single HTTP GET against yandex.ru/sprav/companies/?no_redirect=1, which
// returns server-rendered HTML listing the user's orgs. We extract:
//
//   - permalink: from the first  /sprav/<digits>/p/edit  href
//   - name:      from the first  <h4 ...CompanyInfoCard-CompanyName...>
//
// Both are stable React class names on this page and survive minor
// styling churn. No CSRF dance required.
//
// Returns ({"",""}, nil) when the user has no Sprav orgs registered.
// Returns an error for transport / auth failures so the caller can fall
// back to a placeholder externalID and retry later.
func (h *OAuthHandler) fetchYandexBusinessInfo(ctx context.Context, cookies []yandexcookies.Cookie) (yandexBusinessInfo, error) {
	const companiesURL = "https://yandex.ru/sprav/companies/?no_redirect=1"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, companiesURL, http.NoBody)
	if err != nil {
		return yandexBusinessInfo{}, err
	}
	req.Header.Set("Cookie", buildCookieHeader(cookies))
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	req.Header.Set("Accept-Language", "ru,en;q=0.5")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return yandexBusinessInfo{}, fmt.Errorf("companies fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return yandexBusinessInfo{}, fmt.Errorf("companies HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4MB

	permMatch := spravPermalinkRegex.FindSubmatch(body)
	if len(permMatch) < 2 {
		// User likely has no orgs — page renders empty state instead of
		// the company-card list.
		return yandexBusinessInfo{}, nil
	}
	permalink := string(permMatch[1])
	if _, perr := strconv.ParseUint(permalink, 10, 64); perr != nil {
		return yandexBusinessInfo{}, fmt.Errorf("permalink not numeric: %q", permalink)
	}

	name := ""
	if nameMatch := spravNameRegex.FindSubmatch(body); len(nameMatch) >= 2 {
		name = strings.TrimSpace(htmlEntityReplacer.Replace(string(nameMatch[1])))
	}

	return yandexBusinessInfo{Permalink: permalink, Name: name}, nil
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
