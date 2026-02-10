package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
func NewIntegrationHandler(integrationService IntegrationService, businessService BusinessService) *IntegrationHandler {
	if integrationService == nil {
		panic("integrationService cannot be nil")
	}
	if businessService == nil {
		panic("businessService cannot be nil")
	}
	return &IntegrationHandler{
		integrationService: integrationService,
		businessService:    businessService,
	}
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
func (h *IntegrationHandler) ConnectIntegration(w http.ResponseWriter, r *http.Request) {
	// Extract platform parameter (for future use)
	_ = chi.URLParam(r, "platform")

	// Return 501 Not Implemented
	writeJSONError(w, http.StatusNotImplemented, "OAuth flow not implemented yet")
}

// DeleteIntegration deletes a platform integration
func (h *IntegrationHandler) DeleteIntegration(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Extract platform parameter
	platform := chi.URLParam(r, "platform")

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

	// Get integration by business and platform
	integration, err := h.integrationService.GetByBusinessAndPlatform(r.Context(), business.ID, platform)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			writeJSONError(w, http.StatusNotFound, "integration not found")
			return
		}
		slog.Error("failed to get integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Delete integration
	err = h.integrationService.Delete(r.Context(), integration.ID)
	if err != nil {
		slog.Error("failed to delete integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return 204 No Content
	writeJSON(w, http.StatusNoContent, nil)
}
