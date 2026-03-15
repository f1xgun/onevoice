package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
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
	businessService    BusinessService
}

// NewIntegrationHandler creates a new integration handler instance
func NewIntegrationHandler(integrationService IntegrationService, businessService BusinessService) (*IntegrationHandler, error) {
	if integrationService == nil {
		return nil, fmt.Errorf("NewIntegrationHandler: integrationService cannot be nil")
	}
	if businessService == nil {
		return nil, fmt.Errorf("NewIntegrationHandler: businessService cannot be nil")
	}
	return &IntegrationHandler{
		integrationService: integrationService,
		businessService:    businessService,
	}, nil
}

// ListIntegrations returns all integrations for the authenticated user's business
func (h *IntegrationHandler) ListIntegrations(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get business for user
	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Get integrations for business
	integrations, err := h.integrationService.ListByBusinessID(r.Context(), business.ID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return integrations (empty array if none)
	writeJSON(w, http.StatusOK, integrations)
}

// ConnectIntegration is a stub for future OAuth flow implementation
// Platform parameter will be available via chi.URLParam(r, "platform") when implemented
func (h *IntegrationHandler) ConnectIntegration(w http.ResponseWriter, r *http.Request) {
	// Return 501 Not Implemented
	writeJSONError(w, http.StatusNotImplemented, "OAuth flow not implemented yet")
}

// DeleteIntegration deletes an integration by ID
func (h *IntegrationHandler) DeleteIntegration(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse integration ID from URL
	idStr := chi.URLParam(r, "id")
	integrationID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid integration ID")
		return
	}

	// Get business for user
	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Verify integration belongs to this business
	integrations, err := h.integrationService.ListByBusinessID(r.Context(), business.ID)
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
