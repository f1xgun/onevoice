package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
)

const maxUploadSize = 5 << 20 // 5 MB

var allowedMimeTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// BusinessService defines the interface for business operations
type BusinessService interface {
	Create(ctx context.Context, business *domain.Business) (*domain.Business, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	Update(ctx context.Context, business *domain.Business) (*domain.Business, error)
	// Phase 16 POLICY-05 additions:
	GetToolApprovals(ctx context.Context, actorUserID uuid.UUID, businessID uuid.UUID) (map[string]domain.ToolFloor, error)
	UpdateToolApprovals(ctx context.Context, actorUserID uuid.UUID, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error
}

// BusinessSyncer syncs updated business data to connected platforms.
type BusinessSyncer interface {
	SyncBusiness(business *domain.Business)
}

// BusinessHandler handles business profile endpoints
type BusinessHandler struct {
	businessService BusinessService
	syncer          BusinessSyncer // optional; may be nil
	validate        *validator.Validate
	storage         storage.Uploader // optional; required only for UploadLogo
	// toolsCache is optional — required only when the caller wires
	// GetBusinessToolApprovals / UpdateBusinessToolApprovals endpoints.
	// When nil, those handlers return 503 since they cannot validate tool
	// names against the live registry.
	toolsCache ToolsCache
}

// ToolsCache is the narrow interface this handler needs from
// *service.ToolsRegistryCache. Declared locally so business_test doesn't
// need to import service for test-only fakes.
type ToolsCache interface {
	Has(toolName string) bool
}

// SetToolsCache wires a tools-registry cache for POLICY-05 validation.
// Called after construction so existing NewBusinessHandler call sites don't
// churn. Safe to call with nil to disable the endpoints.
func (h *BusinessHandler) SetToolsCache(c ToolsCache) {
	h.toolsCache = c
}

// UpdateBusinessRequest represents the business update request
type UpdateBusinessRequest struct {
	Name        string  `json:"name" validate:"required"`
	Category    string  `json:"category"`
	Address     string  `json:"address"`
	Phone       string  `json:"phone"`
	Website     *string `json:"website"`
	Description string  `json:"description"`
}

// NewBusinessHandler creates a new business handler instance.
// syncer may be nil; if provided, it is called asynchronously after each successful update.
// objectStorage may be nil in tests that do not exercise UploadLogo.
func NewBusinessHandler(businessService BusinessService, syncer BusinessSyncer, objectStorage storage.Uploader) (*BusinessHandler, error) {
	if businessService == nil {
		return nil, fmt.Errorf("NewBusinessHandler: businessService cannot be nil")
	}
	return &BusinessHandler{
		businessService: businessService,
		syncer:          syncer,
		validate:        validate,
		storage:         objectStorage,
	}, nil
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

	// Sync business info to connected platforms asynchronously
	if h.syncer != nil {
		go h.syncer.SyncBusiness(updatedBusiness)
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

// UpdateVoiceTone updates the business voice/tone tags (stored in settings).
// Body: {"tones": ["Тёплый", "Дружеский"]}.
// Mirrors UpdateSchedule — settings entries live alongside other settings
// keys so reads of /business return the full dict.
func (h *BusinessHandler) UpdateVoiceTone(w http.ResponseWriter, r *http.Request) {
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
		Tones []string `json:"tones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if business.Settings == nil {
		business.Settings = make(map[string]interface{})
	}
	business.Settings["voiceTone"] = req.Tones
	business.UpdatedAt = time.Now()

	updated, err := h.businessService.Update(r.Context(), business)
	if err != nil {
		slog.Error("failed to update voice tone", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// GetBusinessToolApprovals handles GET /api/v1/business/{id}/tool-approvals.
// Response shape: `{"toolApprovals": {"tool_name": "auto"|"manual", ...}}`.
// Absence from the map means the registry floor applies (POLICY-01).
func (h *BusinessHandler) GetBusinessToolApprovals(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	idStr := chi.URLParam(r, "id")
	businessID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid business id")
		return
	}

	approvals, err := h.businessService.GetToolApprovals(r.Context(), userID, businessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			// 404 even when cross-tenant — avoid enumeration per docs/security.md.
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "get tool approvals failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Serialize as `{"toolApprovals": {...}}` — stable field name so the
	// Phase 17 frontend can bind directly.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"toolApprovals": approvals,
	})
}

// updateToolApprovalsRequest is the PUT body shape. Values are strings
// (Auto/Manual); handler converts to ToolFloor after validation.
type updateToolApprovalsRequest struct {
	ToolApprovals map[string]string `json:"toolApprovals"`
}

// UpdateBusinessToolApprovals handles PUT /api/v1/business/{id}/tool-approvals.
// Validation layers:
//  1. JSON shape
//  2. Every key must exist in the live orchestrator registry (via toolsCache).
//     Unknown key → 400 {"error":"unknown tool: X"}
//  3. Every value must be "auto" or "manual" (NOT "forbidden" — that's a
//     registration-time property, not a user setting).
//  4. Ownership: actor's userID matches business.UserID.
func (h *BusinessHandler) UpdateBusinessToolApprovals(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	idStr := chi.URLParam(r, "id")
	businessID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid business id")
		return
	}

	if h.toolsCache == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "tool registry unavailable")
		return
	}

	var req updateToolApprovalsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	approvals := make(map[string]domain.ToolFloor, len(req.ToolApprovals))
	for toolName, floorStr := range req.ToolApprovals {
		if !h.toolsCache.Has(toolName) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "unknown tool: " + toolName,
			})
			return
		}
		floor := domain.ToolFloor(floorStr)
		// POLICY-05: only Auto and Manual are user-settable. Forbidden is a
		// registration-time property — allowing it here would let a user
		// escalate a tool's floor, which matches the registry's floor
		// invariant but would surprise operators and invite confusion.
		if floor != domain.ToolFloorAuto && floor != domain.ToolFloorManual {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid floor for tool " + toolName + ": must be auto or manual",
			})
			return
		}
		approvals[toolName] = floor
	}

	if err := h.businessService.UpdateToolApprovals(r.Context(), userID, businessID, approvals); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "update tool approvals failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"toolApprovals": approvals,
	})
}

// UploadLogo handles multipart logo upload, stores the file in object storage,
// and updates the business logo_url to the public URL.
func (h *BusinessHandler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if h.storage == nil {
		slog.Error("upload logo: object storage is not configured")
		writeJSONError(w, http.StatusInternalServerError, "storage unavailable")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeJSONError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}

	file, header, err := r.FormFile("logo")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "logo field is required")
		return
	}
	defer func() { _ = file.Close() }()

	// Detect MIME type from first 512 bytes
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	mimeType := http.DetectContentType(buf[:n])
	ext, ok := allowedMimeTypes[mimeType]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "unsupported file type: "+mimeType)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("upload logo: get business failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Cache-bust on re-upload by including UpdatedAt nanos in the key.
	key := fmt.Sprintf("businesses/%s/logo-%d%s", business.ID, time.Now().UnixNano(), ext)
	if err := h.storage.Upload(r.Context(), key, file, header.Size, mimeType); err != nil {
		slog.Error("upload logo: storage upload failed", "key", key, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	business.LogoURL = h.storage.PublicURL(key)
	business.UpdatedAt = time.Now()
	updatedBusiness, err := h.businessService.Update(r.Context(), business)
	if err != nil {
		slog.Error("upload logo: update business failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if h.syncer != nil {
		go h.syncer.SyncBusiness(updatedBusiness)
	}

	writeJSON(w, http.StatusOK, updatedBusiness)
}
