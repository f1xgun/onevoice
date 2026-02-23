package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// BusinessService defines the interface for business operations
type BusinessService interface {
	Create(ctx context.Context, business *domain.Business) (*domain.Business, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	Update(ctx context.Context, business *domain.Business) (*domain.Business, error)
}

// BusinessSyncer syncs updated business data to connected platforms.
type BusinessSyncer interface {
	SyncDescription(businessID uuid.UUID, description string)
}

// BusinessHandler handles business profile endpoints
type BusinessHandler struct {
	businessService BusinessService
	syncer          BusinessSyncer // optional; may be nil
	validate        *validator.Validate
}

// UpdateBusinessRequest represents the business update request
type UpdateBusinessRequest struct {
	Name        string `json:"name" validate:"required"`
	Category    string `json:"category"`
	Address     string `json:"address"`
	Phone       string `json:"phone"`
	Website     string `json:"website"`
	Description string `json:"description"`
}

// NewBusinessHandler creates a new business handler instance.
// syncer may be nil; if provided, it is called asynchronously after each successful update.
func NewBusinessHandler(businessService BusinessService, syncer BusinessSyncer) *BusinessHandler {
	if businessService == nil {
		panic("businessService cannot be nil")
	}
	return &BusinessHandler{
		businessService: businessService,
		syncer:          syncer,
		validate:        validate,
	}
}

// GetBusiness returns the business profile for the authenticated user
func (h *BusinessHandler) GetBusiness(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get business from service
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

	// Return business
	writeJSON(w, http.StatusOK, business)
}

// UpdateBusiness updates the business profile for the authenticated user
func (h *BusinessHandler) UpdateBusiness(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse request body
	var req UpdateBusinessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate request
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	// Get existing business (if exists)
	business, err := h.businessService.GetByUserID(r.Context(), userID)

	if err != nil && !errors.Is(err, domain.ErrBusinessNotFound) {
		slog.Error("failed to get business for update", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Create new business if doesn't exist
	if errors.Is(err, domain.ErrBusinessNotFound) {
		newBusiness := &domain.Business{
			ID:          uuid.New(),
			UserID:      userID,
			Name:        req.Name,
			Category:    req.Category,
			Address:     req.Address,
			Phone:       req.Phone,
			Website:     req.Website,
			Description: req.Description,
			Settings:    map[string]interface{}{}, // Initialize empty settings
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		createdBusiness, err := h.businessService.Create(r.Context(), newBusiness)
		if err != nil {
			slog.Error("failed to create business", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		// Return created business
		writeJSON(w, http.StatusCreated, createdBusiness)
		return
	}

	// Update existing business fields from request
	business.Name = req.Name
	business.Category = req.Category
	business.Address = req.Address
	business.Phone = req.Phone
	business.Website = req.Website
	business.Description = req.Description
	business.UpdatedAt = time.Now()

	// Update business
	updatedBusiness, err := h.businessService.Update(r.Context(), business)
	if err != nil {
		slog.Error("failed to update business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Sync description to connected platforms asynchronously
	if h.syncer != nil {
		go h.syncer.SyncDescription(updatedBusiness.ID, updatedBusiness.Description)
	}

	// Return updated business
	writeJSON(w, http.StatusOK, updatedBusiness)
}

// UpdateSchedule updates the business schedule (stored in settings)
func (h *BusinessHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

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

	var req struct {
		Schedule interface{} `json:"schedule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if business.Settings == nil {
		business.Settings = make(map[string]interface{})
	}
	business.Settings["schedule"] = req.Schedule
	business.UpdatedAt = time.Now()

	updated, err := h.businessService.Update(r.Context(), business)
	if err != nil {
		slog.Error("failed to update schedule", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}
