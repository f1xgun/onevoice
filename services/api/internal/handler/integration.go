package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
}

// NewIntegrationHandler creates a new integration handler instance
func NewIntegrationHandler(integrationService IntegrationService, _ BusinessService) (*IntegrationHandler, error) {
	if integrationService == nil {
		return nil, fmt.Errorf("NewIntegrationHandler: integrationService cannot be nil")
	}
	return &IntegrationHandler{
		integrationService: integrationService,
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

	// Verify integration belongs to this business
	integrations, err := h.integrationService.ListByBusinessID(r.Context(), bc.BusinessID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	found := false
	for _, i := range integrations {
		if i.ID == integrationID {
			found = true
			break
		}
	}
	if !found {
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

	// Return 204 No Content
	writeJSON(w, http.StatusNoContent, nil)
}
