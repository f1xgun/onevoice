package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

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

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  business.ID,
		Platform:    "yandex_business",
		ExternalID:  "default",
		AccessToken: parsed.JSON(),
		Metadata: map[string]interface{}{
			"input_format": parsed.Format,
			"connected_at": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		slog.Error("failed to connect Yandex.Business integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	writeJSON(w, http.StatusCreated, integration)
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

// probeYandexSession attempts a one-shot HTTPS request to passport.yandex.ru
// to determine whether the supplied cookies represent a live session.
//
// Signal:
//   - 200 OK + Yandex profile JSON → session valid, parse out display name.
//   - 302/303 to passport.yandex.ru/auth* → not logged in.
//   - Anything else (403/captcha/network) → return error so caller treats
//     the verdict as "unknown".
//
// We intentionally do not follow redirects — Yandex's profile endpoint
// always 302's unauthenticated visitors to /auth, and a single hop is
// the cleanest probe signal.
func (h *OAuthHandler) probeYandexSession(ctx context.Context, cookies []yandexcookies.Cookie) (valid bool, username string, err error) {
	profileURL := h.yandexProbeURL()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, http.NoBody)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Cookie", buildCookieHeader(cookies))
	// Realistic UA reduces the chance of being served a captcha-gate page.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json,text/html,*/*")
	req.Header.Set("Accept-Language", "ru,en;q=0.5")

	// Don't follow redirects — we want to see the 302 itself.
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
		// Some other redirect — inconclusive.
		return false, "", errors.New("unexpected redirect: " + loc)

	case resp.StatusCode == http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return true, parseYandexUsername(body), nil

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

// usernameRegex pulls a "display_name":"..." or "login":"..." field out of
// the passport profile JSON response. Yandex's HTML profile page also
// embeds a JSON blob with the same keys, so the same regex covers both
// the JSON and HTML cases.
var usernameRegex = regexp.MustCompile(`"(?:display_name|login)"\s*:\s*"([^"]+)"`)

func parseYandexUsername(body []byte) string {
	m := usernameRegex.FindSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return string(m[1])
}

// yandexProbeURL returns the live-probe endpoint, honoring an optional
// test override on OAuthConfig.
func (h *OAuthHandler) yandexProbeURL() string {
	if h.cfg.yandexProbeBaseURL != "" {
		return h.cfg.yandexProbeBaseURL
	}
	return "https://yandex.ru/api/passport"
}
