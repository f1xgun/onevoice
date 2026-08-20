package oauth

import (
	"context"
	"encoding/base64"
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
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/yandexcookies"
)

// optStr returns a *string pointing at s, or nil when s is empty. Used
// for spec-side response fields that are optional (*string with omitempty).
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// optStrings returns a *[]string pointing at s, or nil when s is empty.
// Used for spec-side response fields that are optional (*[]string).
func optStrings(s []string) *[]string {
	if len(s) == 0 {
		return nil
	}
	return &s
}

// ProbeYandexBusiness validates pasted cookies without persisting; always 200.
// See docs/api/handlers/oauth-yandex-connect.md §"Probe".
func (h *OAuthHandler) ProbeYandexBusiness(w http.ResponseWriter, r *http.Request) {
	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req openapi.YandexCookiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, openapi.YandexProbeResponse{
			Ok:    false,
			Error: optStr(i18n.Tr(r.Context(), "oauth.yandex.invalid_body")),
		})
		return
	}

	parsed, err := yandexcookies.Parse(req.Cookies)
	if err != nil {
		writeJSON(w, http.StatusOK, openapi.YandexProbeResponse{
			Ok:    false,
			Error: optStr(yandexCookiesErrorMessage(r, err)),
		})
		return
	}

	resp := openapi.YandexProbeResponse{
		Ok:       true,
		Format:   optStr(parsed.Format),
		Warnings: optStrings(cookieWarnings(r, parsed.Cookies)),
	}

	probeCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	valid, username, probeErr := h.probeYandexSession(probeCtx, parsed.Cookies)
	if probeErr != nil {
		slog.Info("yandex session probe inconclusive", "error", probeErr)
	} else {
		resp.SessionValid = &valid
		if valid {
			resp.Username = optStr(username)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ConnectYandexBusiness persists pasted cookies as a new integration.
// See docs/api/handlers/oauth-yandex-connect.md §"Connect".
func (h *OAuthHandler) ConnectYandexBusiness(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ConnectYandexBusiness: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req openapi.ConnectYandexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	parsed, err := yandexcookies.Parse(req.Cookies)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, yandexCookiesErrorMessage(r, err))
		return
	}

	externalID := "default"
	if req.Permalink != nil {
		if p := strings.TrimSpace(*req.Permalink); p != "" {
			externalID = p
		}
	}
	metadata := map[string]any{
		"input_format": parsed.Format,
		"connected_at": time.Now().UTC().Format(time.RFC3339),
	}
	if req.BusinessName != nil {
		if name := strings.TrimSpace(*req.BusinessName); name != "" {
			metadata["business_name"] = name
		}
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:   bc.BusinessID,
		ActorID:      bc.UserID,
		Platform:     a2a.AgentYandexBusiness,
		ExternalID:   externalID,
		AccessToken:  parsed.JSON(),
		Metadata:     metadata,
		ActorIP:      middleware.ClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
		ParsedFormat: parsed.Format,
	})
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to connect Yandex.Business integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	writeJSON(w, http.StatusCreated, integration)
}

// yandexCookiesArg builds the list_companies tool argument carrying the pasted
// session cookies. With a payload encryptor configured the cookies are sealed
// (AES-256-GCM, base64) under the "cookies_enc" key so the secret never crosses
// the NATS bus in the clear; the agent decrypts with the shared key. Without an
// encryptor it falls back to the plaintext "cookies" key (dev), unchanged.
func (h *OAuthHandler) yandexCookiesArg(cookiesJSON string) (map[string]any, error) {
	if h.payloadEnc == nil {
		return map[string]any{"cookies": cookiesJSON}, nil
	}
	sealed, err := h.payloadEnc.Encrypt([]byte(cookiesJSON))
	if err != nil {
		return nil, err
	}
	return map[string]any{"cookies_enc": base64.StdEncoding.EncodeToString(sealed)}, nil
}

// yandexListCompaniesTimeout caps the agent's list_companies RPA (Playwright
// SPA hydration ~25-45s).
const yandexListCompaniesTimeout = 60 * time.Second

// ListYandexCompanies dispatches the list_companies RPA and returns picker rows.
// Synchronous; blocks the request for the full Playwright run.
// See docs/api/handlers/oauth-yandex-connect.md §"List companies".
func (h *OAuthHandler) ListYandexCompanies(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ListYandexCompanies: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	if h.taskPublisher == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "agent task publisher not configured")
		return
	}

	var req openapi.YandexCookiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	parsed, err := yandexcookies.Parse(req.Cookies)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, yandexCookiesErrorMessage(r, err))
		return
	}

	args, err := h.yandexCookiesArg(parsed.JSON())
	if err != nil {
		slog.Error("yandex list companies: encrypt cookies", "error", err)
		writeJSONErrorKey(w, r, http.StatusInternalServerError, "oauth.yandex.list_orgs_failed")
		return
	}
	toolReq := a2a.ToolRequest{
		TaskID:     uuid.NewString(),
		Tool:       tools.YandexBusinessListCompanies,
		Args:       args,
		BusinessID: bc.BusinessID.String(),
	}
	resp, callErr := h.taskPublisher.RequestTool(r.Context(), a2a.Subject(a2a.AgentYandexBusiness), toolReq, yandexListCompaniesTimeout)
	if callErr != nil {
		slog.Info("yandex list companies: agent call failed", "business_id", bc.BusinessID, "error", callErr)
		writeJSONErrorKey(w, r, http.StatusBadGateway, "oauth.yandex.list_orgs_failed")
		return
	}
	if resp == nil || resp.Error != "" {
		agentErr := "nil response"
		if resp != nil {
			agentErr = resp.Error
		}
		slog.Info("yandex list companies: agent returned error", "business_id", bc.BusinessID, "error", agentErr)
		writeJSONErrorKey(w, r, http.StatusBadGateway, "oauth.yandex.list_orgs_failed")
		return
	}

	companiesRaw, _ := resp.Result["companies"].([]any)
	companies := make([]openapi.YandexCompanyEntry, 0, len(companiesRaw))
	for _, c := range companiesRaw {
		row, _ := c.(map[string]any)
		permalink, _ := row["permalink"].(string)
		name, _ := row["name"].(string)
		permalink = strings.TrimSpace(permalink)
		name = strings.TrimSpace(name)
		if permalink == "" {
			continue
		}
		companies = append(companies, openapi.YandexCompanyEntry{Permalink: permalink, Name: name})
	}
	writeJSON(w, http.StatusOK, openapi.YandexCompaniesResponse{Companies: companies})
}

// RefreshYandexBusinessName backfills metadata.business_name (and heals
// legacy external_id) async via Playwright RPA. Returns 202 immediately.
// See docs/api/handlers/oauth-yandex-connect.md §"Refresh name".
func (h *OAuthHandler) RefreshYandexBusinessName(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "RefreshYandexBusinessName: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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

	integrations, err := h.integrationService.ListByBusinessAndPlatform(r.Context(), bc.BusinessID, a2a.AgentYandexBusiness)
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

	bgCtx, bgCancel := context.WithTimeout(context.Background(), yandexListCompaniesTimeout+15*time.Second)
	go h.runYandexListCompaniesRefresh(bgCtx, bgCancel, integrationID, *target, bc.BusinessID)

	writeJSON(w, http.StatusAccepted, openapi.RefreshStartedResponse{
		Status: openapi.RefreshStarted,
	})
}

// runYandexListCompaniesRefresh performs the detached RPA + metadata writes,
// healing legacy external_id="default" rows. See docs/api/handlers/oauth-yandex-connect.md.
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
		Tool:       tools.YandexBusinessListCompanies,
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

	if permalink != "" && permalink != target.ExternalID {
		if updateErr := h.integrationService.UpdateExternalID(ctx, integrationID, permalink); updateErr != nil {
			slog.Error("yandex name refresh: failed to persist healed external_id",
				"integration_id", integrationID, "from", target.ExternalID, "to", permalink, "error", updateErr)
		} else {
			slog.Info("yandex name refresh: healed external_id",
				"integration_id", integrationID, "from", target.ExternalID, "to", permalink)
			target.ExternalID = permalink
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

// cookieWarnings flags missing-but-recommended cookies (sessionid2,
// yandex_login). See docs/api/handlers/oauth-yandex-connect.md §"Cookie warnings".
func cookieWarnings(r *http.Request, cookies []yandexcookies.Cookie) []string {
	have := map[string]bool{}
	for _, c := range cookies {
		have[strings.ToLower(c.Name)] = true
	}
	var warnings []string
	if !have["sessionid2"] {
		warnings = append(warnings, i18n.Tr(r.Context(), "oauth.yandex.missing_sessionid2"))
	}
	if !have["yandex_login"] {
		warnings = append(warnings, i18n.Tr(r.Context(), "oauth.yandex.missing_yandex_login"))
	}
	return warnings
}

// yandexCookiesErrorMessage maps yandexcookies.Parse errors to localized
// strings via pkg/i18n. See docs/api/handlers/oauth-yandex-connect.md §"Probe".
func yandexCookiesErrorMessage(r *http.Request, err error) string {
	ctx := r.Context()
	switch {
	case errors.Is(err, yandexcookies.ErrEmpty):
		return i18n.Tr(ctx, "yandex.cookies.empty")
	case errors.Is(err, yandexcookies.ErrNoSessionID):
		return i18n.Tr(ctx, "yandex.cookies.missing_sessionid")
	case errors.Is(err, yandexcookies.ErrInvalidJSON):
		return i18n.Tr(ctx, "yandex.cookies.invalid_format")
	case errors.Is(err, yandexcookies.ErrSessionIDInvalid):
		return i18n.Tr(ctx, "yandex.cookies.invalid_sessionid")
	case errors.Is(err, yandexcookies.ErrJSONUnmarshal):
		detail := strings.TrimPrefix(err.Error(), yandexcookies.ErrJSONUnmarshal.Error()+": ")
		return i18n.Tr(ctx, "yandex.cookies.json_error", detail)
	default:
		return err.Error()
	}
}

// probeYandexSession checks whether the supplied cookies form a live session
// by watching for the passport.yandex.ru redirect.
// See docs/api/handlers/oauth-yandex-connect.md §"Live session probe".
func (h *OAuthHandler) probeYandexSession(ctx context.Context, cookies []yandexcookies.Cookie) (valid bool, username string, err error) {
	probeURL := h.yandexProbeURL()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, http.NoBody)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Cookie", buildCookieHeader(cookies))
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	req.Header.Set("Accept-Language", "ru,en;q=0.5")

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

// yandexProbeURL returns the probe endpoint (test override via cfg).
func (h *OAuthHandler) yandexProbeURL() string {
	if h.cfg.yandexProbeBaseURL != "" {
		return h.cfg.yandexProbeBaseURL
	}
	return defaultYandexProbeURL
}
