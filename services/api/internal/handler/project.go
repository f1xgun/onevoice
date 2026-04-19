package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// ProjectHandler serves the /api/v1/projects REST endpoints.
type ProjectHandler struct {
	projectService  ProjectService
	businessService BusinessService
	// toolsCache is optional — required only for POLICY-06 approval-overrides
	// validation on PUT /projects/{id}. When nil, approvalOverrides in the
	// request body are rejected with a 503.
	toolsCache ToolsCache
}

// NewProjectHandler constructs a ProjectHandler. Both dependencies are
// required; a nil businessService would break the business-scoping invariant
// that prevents cross-tenant project access.
func NewProjectHandler(ps ProjectService, bs BusinessService) (*ProjectHandler, error) {
	if ps == nil {
		return nil, fmt.Errorf("NewProjectHandler: projectService cannot be nil")
	}
	if bs == nil {
		return nil, fmt.Errorf("NewProjectHandler: businessService cannot be nil")
	}
	return &ProjectHandler{
		projectService:  ps,
		businessService: bs,
	}, nil
}

// SetToolsCache wires a tools-registry cache so PUT /projects/{id} can
// validate approvalOverrides keys against the live orchestrator registry
// (POLICY-06). Safe to call with nil to disable the field.
func (h *ProjectHandler) SetToolsCache(c ToolsCache) {
	h.toolsCache = c
}

// projectRequest is the JSON shape consumed by both Create and Update —
// Plan 15-CONTEXT D-02 says the same form handles both operations.
//
// Phase 16 (POLICY-06) — ApprovalOverrides is a map of tool names to floor
// strings. Valid values are "auto" | "manual" | "inherit". "inherit" is
// stripped from the map before persistence — key-absence is the canonical
// encoding of inherit (Overview invariant #8). The handler does this
// translation in buildApprovalOverrides() below.
type projectRequest struct {
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	SystemPrompt      string            `json:"systemPrompt"`
	WhitelistMode     string            `json:"whitelistMode"`
	AllowedTools      []string          `json:"allowedTools"`
	ApprovalOverrides map[string]string `json:"approvalOverrides"`
	QuickActions      []string          `json:"quickActions"`
}

// toInput converts the wire-format request into the service-layer input struct.
// The service layer owns validation of name/prompt-length/mode/empty-explicit.
// Edit-field validation for approvalOverrides lives in the handler since it
// requires the tools cache which the service doesn't have.
func (req projectRequest) toInput(overrides map[string]domain.ToolFloor) service.CreateProjectInput {
	return service.CreateProjectInput{
		Name:              req.Name,
		Description:       req.Description,
		SystemPrompt:      req.SystemPrompt,
		WhitelistMode:     domain.WhitelistMode(req.WhitelistMode),
		AllowedTools:      req.AllowedTools,
		ApprovalOverrides: overrides,
		QuickActions:      req.QuickActions,
	}
}

// buildApprovalOverrides validates and strips inherit-valued entries from the
// request's approvalOverrides map. Returns (overrides, httpStatus, errorBody).
//  - Unknown tool name → (nil, 400, {"error":"unknown tool: X"})
//  - Invalid value (not in {auto,manual,inherit}) → (nil, 400, {"error":"..."})
//  - "inherit" → key stripped from the returned map (inherit == absence)
// When the handler's toolsCache is nil, returns (nil, 503, ...) because we
// cannot validate without the live registry.
func (h *ProjectHandler) buildApprovalOverrides(body map[string]string) (map[string]domain.ToolFloor, int, map[string]string) {
	if len(body) == 0 {
		return nil, 0, nil
	}
	if h.toolsCache == nil {
		return nil, http.StatusServiceUnavailable, map[string]string{"error": "tool registry unavailable"}
	}
	out := make(map[string]domain.ToolFloor, len(body))
	for toolName, val := range body {
		if !h.toolsCache.Has(toolName) {
			return nil, http.StatusBadRequest, map[string]string{"error": "unknown tool: " + toolName}
		}
		switch val {
		case "auto":
			out[toolName] = domain.ToolFloorAuto
		case "manual":
			out[toolName] = domain.ToolFloorManual
		case "inherit":
			// Key absence encoding — do not persist.
			continue
		default:
			return nil, http.StatusBadRequest, map[string]string{
				"error": "invalid floor for tool " + toolName + ": must be auto, manual, or inherit",
			}
		}
	}
	return out, 0, nil
}

// mapProjectError translates service/domain errors into HTTP status codes and
// user-facing messages. Centralized so every endpoint has identical mapping.
func (h *ProjectHandler) mapProjectError(ctx context.Context, w http.ResponseWriter, err error, op string) {
	switch {
	case errors.Is(err, domain.ErrProjectNotFound):
		writeJSONError(w, http.StatusNotFound, "project not found")
	case errors.Is(err, domain.ErrProjectExists):
		writeJSONError(w, http.StatusConflict, "project already exists")
	case errors.Is(err, domain.ErrProjectNameRequired),
		errors.Is(err, domain.ErrProjectSystemPromptTooLong),
		errors.Is(err, domain.ErrProjectWhitelistEmpty),
		errors.Is(err, domain.ErrProjectWhitelistMode):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		slog.ErrorContext(ctx, "project handler error", "op", op, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
	}
}

// resolveBusinessID pulls the userID from the auth middleware context and
// maps it to the caller's business. Returns (uuid.Nil, false) after writing
// the appropriate HTTP error response.
func (h *ProjectHandler) resolveBusinessID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return uuid.Nil, false
	}
	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return uuid.Nil, false
		}
		slog.ErrorContext(r.Context(), "failed to resolve business for project endpoint", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return uuid.Nil, false
	}
	return business.ID, true
}

// parseProjectID extracts and validates the {id} URL param.
func parseProjectID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "id")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid project id")
		return uuid.Nil, false
	}
	return id, true
}

// Create handles POST /api/v1/projects.
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	businessID, ok := h.resolveBusinessID(w, r)
	if !ok {
		return
	}

	var req projectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	overrides, status, errBody := h.buildApprovalOverrides(req.ApprovalOverrides)
	if status != 0 {
		writeJSON(w, status, errBody)
		return
	}

	project, err := h.projectService.Create(r.Context(), businessID, req.toInput(overrides))
	if err != nil {
		h.mapProjectError(r.Context(), w, err, "create")
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

// List handles GET /api/v1/projects.
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	businessID, ok := h.resolveBusinessID(w, r)
	if !ok {
		return
	}

	projects, err := h.projectService.ListByBusinessID(r.Context(), businessID)
	if err != nil {
		h.mapProjectError(r.Context(), w, err, "list")
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

// Get handles GET /api/v1/projects/{id}.
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	businessID, ok := h.resolveBusinessID(w, r)
	if !ok {
		return
	}
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}

	project, err := h.projectService.GetByID(r.Context(), businessID, id)
	if err != nil {
		h.mapProjectError(r.Context(), w, err, "get")
		return
	}
	writeJSON(w, http.StatusOK, project)
}

// Update handles PUT /api/v1/projects/{id}.
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	businessID, ok := h.resolveBusinessID(w, r)
	if !ok {
		return
	}
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}

	var req projectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	overrides, status, errBody := h.buildApprovalOverrides(req.ApprovalOverrides)
	if status != 0 {
		writeJSON(w, status, errBody)
		return
	}

	project, err := h.projectService.Update(r.Context(), businessID, id, req.toInput(overrides))
	if err != nil {
		h.mapProjectError(r.Context(), w, err, "update")
		return
	}
	writeJSON(w, http.StatusOK, project)
}

// Delete handles DELETE /api/v1/projects/{id}. Hard-deletes the project plus
// every Mongo conversation/message assigned to it (D-05), returning the
// counts so the frontend can show "deleted N chats" feedback.
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	businessID, ok := h.resolveBusinessID(w, r)
	if !ok {
		return
	}
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}

	convs, msgs, err := h.projectService.DeleteCascade(r.Context(), businessID, id)
	if err != nil {
		h.mapProjectError(r.Context(), w, err, "delete")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{
		"deletedConversations": convs,
		"deletedMessages":      msgs,
	})
}

// ConversationCount handles GET /api/v1/projects/{id}/conversation-count.
// Feeds the frontend delete-confirmation dialog (D-06) so the user sees how
// many chats will be destroyed before confirming.
func (h *ProjectHandler) ConversationCount(w http.ResponseWriter, r *http.Request) {
	businessID, ok := h.resolveBusinessID(w, r)
	if !ok {
		return
	}
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}

	count, err := h.projectService.CountConversations(r.Context(), businessID, id)
	if err != nil {
		h.mapProjectError(r.Context(), w, err, "conversation-count")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}
