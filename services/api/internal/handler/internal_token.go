package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/google/uuid"
)

// TokenService defines the interface for decrypted token retrieval
type TokenService interface {
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*service.TokenResponse, error)
}

// InternalTokenHandler handles internal token endpoints consumed by agents
type InternalTokenHandler struct {
	tokenService TokenService
}

// NewInternalTokenHandler creates a new InternalTokenHandler instance
func NewInternalTokenHandler(tokenService TokenService) *InternalTokenHandler {
	return &InternalTokenHandler{tokenService: tokenService}
}

// GetToken handles GET /internal/v1/tokens
// Query params: business_id (required), platform (required), external_id (optional)
func (h *InternalTokenHandler) GetToken(w http.ResponseWriter, r *http.Request) {
	businessIDStr := r.URL.Query().Get("business_id")
	platform := r.URL.Query().Get("platform")
	externalID := r.URL.Query().Get("external_id")

	if businessIDStr == "" || platform == "" {
		writeJSONError(w, http.StatusBadRequest, "business_id and platform are required")
		return
	}

	businessID, err := uuid.Parse(businessIDStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid business_id")
		return
	}

	token, err := h.tokenService.GetDecryptedToken(r.Context(), businessID, platform, externalID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			writeJSONError(w, http.StatusNotFound, "integration not found")
			return
		}
		if errors.Is(err, domain.ErrTokenExpired) {
			writeJSONError(w, http.StatusGone, "token expired, refresh failed")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, token)
}
