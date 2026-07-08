package oauth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/yandexorg"
)

// yandexVerifyAccessTimeout caps the verify_access RPA (Playwright SPA
// hydration + the direct edit-page probe).
const yandexVerifyAccessTimeout = 60 * time.Second

// delegatedConfigured reports whether the delegated-representative access plane
// is provisioned: both the shared representative login (shown to owners) and the
// shared-session sentinel business must be set. When either is empty the
// delegated endpoints fail closed with a clear "not configured" error and
// nothing else changes.
func (h *OAuthHandler) delegatedConfigured() bool {
	return strings.TrimSpace(h.cfg.YandexRepLogin) != "" && strings.TrimSpace(h.cfg.YandexSharedBusinessID) != ""
}

// ConnectDelegatedYandexBusiness stores a permalink-only delegated integration.
// The owner provides only their org permalink (or a Maps/Sprav URL); no customer
// credential is captured. The integration carries an empty access token,
// metadata.connect_mode="delegated", and access_verified=false until a
// subsequent verify-access confirms the shared representative can reach the org.
func (h *OAuthHandler) ConnectDelegatedYandexBusiness(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ConnectDelegatedYandexBusiness: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.delegatedConfigured() {
		writeJSONError(w, http.StatusServiceUnavailable, "delegated access not configured")
		return
	}

	var req openapi.ConnectDelegatedYandexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	permalink, err := resolveDelegatedPermalink(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid or missing permalink")
		return
	}

	metadata := map[string]any{
		"connect_mode":    tools.ConnectModeDelegated,
		"access_verified": false,
		"connected_at":    time.Now().UTC().Format(time.RFC3339),
	}
	if req.BusinessName != nil {
		if name := strings.TrimSpace(*req.BusinessName); name != "" {
			metadata["business_name"] = name
		}
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  bc.BusinessID,
		ActorID:     bc.UserID,
		Platform:    a2a.AgentYandexBusiness,
		ExternalID:  permalink,
		AccessToken: "", // delegated: no per-business credential is stored
		Metadata:    metadata,
		ActorIP:     middleware.ClientIP(r),
		UserAgent:   r.Header.Get("User-Agent"),
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrIntegrationClaimedByOtherTenant):
			writeJSONError(w, http.StatusConflict, "permalink already connected to another organization")
			return
		case errors.Is(err, domain.ErrBusinessNotFound):
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		default:
			slog.Error("failed to connect delegated Yandex integration", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to connect")
			return
		}
	}

	writeJSON(w, http.StatusCreated, integration)
}

// VerifyYandexBusinessAccess dispatches a verify_access RPA over the SHARED
// representative session and reports whether the org's edit page mounts (access
// confirmed). On confirmation it stamps metadata.access_verified=true on the
// business's delegated integration for that permalink. The permalink is resolved
// exclusively from the request + confirmed against the business's own
// integration row — the isolation invariant.
func (h *OAuthHandler) VerifyYandexBusinessAccess(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "VerifyYandexBusinessAccess: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.delegatedConfigured() {
		writeJSONError(w, http.StatusServiceUnavailable, "delegated access not configured")
		return
	}
	if h.taskPublisher == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "agent task publisher not configured")
		return
	}

	var req openapi.VerifyYandexAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	permalink, err := yandexorg.ParsePermalink(req.Permalink)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid or missing permalink")
		return
	}

	target := h.findDelegatedIntegration(r, bc.BusinessID, permalink)
	if target == nil {
		writeJSONError(w, http.StatusNotFound, "no delegated integration for this permalink")
		return
	}

	toolReq := a2a.ToolRequest{
		TaskID:     uuid.NewString(),
		Tool:       tools.YandexBusinessVerifyAccess,
		Args:       map[string]any{"permalink": permalink},
		BusinessID: bc.BusinessID.String(),
	}
	resp, callErr := h.taskPublisher.RequestTool(r.Context(), a2a.Subject(a2a.AgentYandexBusiness), toolReq, yandexVerifyAccessTimeout)
	if callErr != nil {
		slog.Info("yandex verify access: agent call failed", "business_id", bc.BusinessID, "error", callErr)
		writeJSONError(w, http.StatusBadGateway, "verification failed")
		return
	}
	if resp == nil || resp.Error != "" {
		errMsg := "agent error"
		if resp != nil && resp.Error != "" {
			errMsg = resp.Error
		}
		writeJSONError(w, http.StatusBadGateway, errMsg)
		return
	}

	detected, _ := resp.Result["access_verified"].(bool)
	if detected {
		h.markAccessVerified(r, *target)
	}

	writeJSON(w, http.StatusOK, openapi.VerifyYandexAccessResponse{AccessVerified: detected})
}

// findDelegatedIntegration returns the business's delegated Yandex integration
// whose external_id matches the permalink, or nil. It enforces isolation: verify
// can only target a permalink this business actually owns a delegated row for.
func (h *OAuthHandler) findDelegatedIntegration(r *http.Request, businessID uuid.UUID, permalink string) *domain.Integration {
	integrations, err := h.integrationService.ListByBusinessAndPlatform(r.Context(), businessID, a2a.AgentYandexBusiness)
	if err != nil {
		slog.ErrorContext(r.Context(), "verify access: list integrations failed", "error", err)
		return nil
	}
	for i := range integrations {
		if integrations[i].ExternalID != permalink {
			continue
		}
		if mode, _ := integrations[i].Metadata["connect_mode"].(string); mode != tools.ConnectModeDelegated {
			continue
		}
		return &integrations[i]
	}
	return nil
}

// markAccessVerified stamps metadata.access_verified=true on the delegated
// integration, preserving the rest of its metadata.
func (h *OAuthHandler) markAccessVerified(r *http.Request, target domain.Integration) {
	metadata := map[string]any{}
	for k, v := range target.Metadata {
		metadata[k] = v
	}
	metadata["access_verified"] = true
	if err := h.integrationService.UpdateMetadata(r.Context(), target.ID, metadata); err != nil {
		slog.ErrorContext(r.Context(), "verify access: failed to persist access_verified",
			"integration_id", target.ID, "error", err)
	}
}

// resolveDelegatedPermalink extracts the numeric permalink from the delegated
// connect request, accepting either a bare/explicit permalink or a pasted
// Maps/Sprav URL.
func resolveDelegatedPermalink(req openapi.ConnectDelegatedYandexRequest) (string, error) {
	if req.Permalink != nil && strings.TrimSpace(*req.Permalink) != "" {
		return yandexorg.ParsePermalink(*req.Permalink)
	}
	if req.MapsUrl != nil && strings.TrimSpace(*req.MapsUrl) != "" {
		return yandexorg.ParsePermalink(*req.MapsUrl)
	}
	return "", yandexorg.ErrEmpty
}
