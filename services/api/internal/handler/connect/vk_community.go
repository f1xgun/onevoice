package connect

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// ConnectVK validates a pasted community access token and stores it as a VK
// integration (JWT required). Two calling shapes are supported:
//
//  1. Token only — handler resolves the bound community via
//     groups.getById?access_token=… (community tokens carry their group
//     identity). Recommended path for the UI paste-flow.
//  2. Token + group_id/URL/screen_name — handler resolves the input through
//     resolveVKGroupID and validates the token against that specific group.
//     Kept for legacy OAuth-callback callers and explicit picker flows.
//
// Token scope is required to include `wall` for review-reply dispatch to work
// — handler refuses up-front so users don't connect a token that will fail
// silently when they try to send an answer.
func (h *ConnectHandler) ConnectVK(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ConnectVK: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req openapi.ConnectVKRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.AccessToken = strings.TrimSpace(req.AccessToken)
	if req.AccessToken == "" {
		writeJSONError(w, http.StatusBadRequest, "access_token is required")
		return
	}

	var groupID string
	if req.GroupId != nil {
		groupID = *req.GroupId
	}
	group, vkErr, transportErr := h.probeVKCommunityToken(r.Context(), req.AccessToken, groupID)
	if transportErr != nil {
		if errors.Is(transportErr, ErrVKCommunityResolveFailed) {
			detail := strings.TrimPrefix(transportErr.Error(), ErrVKCommunityResolveFailed.Error()+": ")
			writeJSONErrorKey(w, r, http.StatusBadRequest, "connect.vk.community_resolve_failed", detail)
			return
		}
		slog.Error("VK token validation failed", "error", transportErr)
		writeJSONError(w, http.StatusBadGateway, "failed to validate VK token")
		return
	}
	if vkErr != "" {
		writeJSONErrorKey(w, r, http.StatusBadRequest, "connect.vk.invalid_token", vkErr)
		return
	}
	if group == nil {
		writeJSONErrorKey(w, r, http.StatusBadRequest, "connect.vk.community_unknown")
		return
	}

	if scopeErr := h.checkVKWallScope(r.Context(), req.AccessToken); scopeErr != nil {
		if errors.Is(scopeErr, ErrVKWallPermissionMissing) {
			writeJSONErrorKey(w, r, http.StatusBadRequest, "connect.vk.wall_permission_missing")
			return
		}
		writeJSONError(w, http.StatusBadRequest, scopeErr.Error())
		return
	}

	groupIDStr := strconv.FormatInt(group.ID, 10)
	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  bc.BusinessID,
		ActorID:     bc.UserID,
		Platform:    a2a.AgentVK,
		ExternalID:  groupIDStr,
		AccessToken: req.AccessToken,
		Metadata: map[string]interface{}{
			"group_id":       groupIDStr,
			"community_name": group.Name,
			"screen_name":    group.ScreenName,
			"photo_url":      group.Photo50,
			"input_method":   "paste",
		},
		ActorIP:      middleware.ClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
		ParsedFormat: "access_token",
	})
	if err != nil {
		slog.Error("failed to connect VK integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	slog.Info("VK community connected via paste",
		"business_id", bc.BusinessID, "group_id", groupIDStr, "name", group.Name)
	writeJSON(w, http.StatusCreated, integration)
}

// RefreshVKCommunityName backfills missing display names on existing VK
// integrations. Fire-and-forget pattern: the frontend triggers this on
// /integrations load whenever a VK row is missing community_name. Returns
// 200 + the updated integration, or 200 + the original integration if the
// VK API call yielded nothing (don't surface transient lookup failures
// as user-visible errors).
func (h *ConnectHandler) RefreshVKCommunityName(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "RefreshVKCommunityName: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsConnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	idStr := chi.URLParam(r, "id")
	integrationID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid integration id")
		return
	}

	integrations, err := h.integrationService.ListByBusinessAndPlatform(r.Context(), bc.BusinessID, a2a.AgentVK)
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

	accessToken := ""
	if tok, tokErr := h.integrationService.GetDecryptedToken(r.Context(), bc.BusinessID, a2a.AgentVK, target.ExternalID, "vk_community_probe"); tokErr == nil && tok != nil {
		accessToken = tok.AccessToken
	}

	name, lookupErr := h.fetchVKCommunityName(r.Context(), target.ExternalID, accessToken)
	if lookupErr != nil {
		slog.Info("VK community name lookup failed; leaving metadata untouched",
			"integration_id", integrationID, "error", lookupErr)
		writeJSON(w, http.StatusOK, target)
		return
	}
	if name == "" {
		writeJSON(w, http.StatusOK, target)
		return
	}

	metadata := map[string]any{}
	for k, v := range target.Metadata {
		metadata[k] = v
	}
	metadata["community_name"] = name

	if updateErr := h.integrationService.UpdateMetadata(r.Context(), integrationID, metadata); updateErr != nil {
		slog.Error("failed to update VK metadata", "error", updateErr)
		writeJSON(w, http.StatusOK, target)
		return
	}
	target.Metadata = metadata
	writeJSON(w, http.StatusOK, target)
}
