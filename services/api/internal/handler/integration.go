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
// auditLogger receives the integration.disconnected
// event emitted from DeleteIntegration. Connected + token_rotated are emitted
// from the service layer (one call site per action — ). nil-safe via
// audit.Nop so existing handler tests that pass nil still work.
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

	integrations, err := h.integrationService.ListByBusinessID(r.Context(), bc.BusinessID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

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

	idStr := chi.URLParam(r, "integrationId")
	integrationID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid integration ID")
		return
	}

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

	err = h.integrationService.Delete(r.Context(), integrationID)
	if err != nil {
		slog.Error("failed to delete integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	audit.LogIntegrationDisconnected(r.Context(), h.audit, bc.BusinessID, bc.UserID, integrationID, target.Platform)

	writeJSON(w, http.StatusNoContent, nil)
}
