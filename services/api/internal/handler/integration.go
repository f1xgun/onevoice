package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// IntegrationService defines the interface for integration operations
type IntegrationService interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error)
	Delete(ctx context.Context, integrationID uuid.UUID) error
}

// IntegrationHandler handles integration endpoints
type IntegrationHandler struct {
	integrationService IntegrationService
	audit              audit.Logger
}

// NewIntegrationHandler creates a new integration handler instance.
//
// Phase 19 Wave 4 (19-04): auditLogger receives the integration.disconnected
// event emitted from DeleteIntegration. Connected + token_rotated are emitted
// from the service layer (one call site per action — D-29). nil-safe via
// audit.Nop() so existing handler tests that pass nil still work.
func NewIntegrationHandler(integrationService IntegrationService, _ BusinessService, auditLogger audit.Logger) (*IntegrationHandler, error) {
	if integrationService == nil {
		return nil, fmt.Errorf("NewIntegrationHandler: integrationService cannot be nil")
	}
	if auditLogger == nil {
		auditLogger = audit.Nop()
	}
	return &IntegrationHandler{
		integrationService: integrationService,
		audit:              auditLogger,
	}, nil
}

// ListIntegrations returns all integrations for the business from the request context.
func (h *IntegrationHandler) ListIntegrations(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ListIntegrations: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Get integrations for business
	integrations, err := h.integrationService.ListByBusinessID(r.Context(), bc.BusinessID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return integrations (empty array if none)
	writeJSON(w, http.StatusOK, integrations)
}

// DeleteIntegration deletes an integration by ID
func (h *IntegrationHandler) DeleteIntegration(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "DeleteIntegration: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermIntegrationsDisconnect) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Parse integration ID from URL
	idStr := chi.URLParam(r, "integrationId")
	integrationID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid integration ID")
		return
	}

	// Verify integration belongs to this business. Also captures the platform
	// for the audit emission below — the service-layer Delete only has the
	// integration_id, so we capture platform HERE before the row is gone.
	integrations, err := h.integrationService.ListByBusinessID(r.Context(), bc.BusinessID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
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

	// Delete integration
	err = h.integrationService.Delete(r.Context(), integrationID)
	if err != nil {
		slog.Error("failed to delete integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Phase 19 audit (D-14, D-29/D-30): emit integration.disconnected AFTER
	// the row is deleted. We captured platform from the pre-delete fetch above
	// so the audit row records what was disconnected. Fire-and-forget.
	audit.LogIntegrationDisconnected(r.Context(), h.audit, bc.BusinessID, bc.UserID, integrationID, target.Platform)

	// Return 204 No Content
	writeJSON(w, http.StatusNoContent, nil)
}
