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

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
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
	Create(ctx context.Context, business *domain.Business, ownerUserID uuid.UUID) (*domain.Business, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	Update(ctx context.Context, business *domain.Business) (*domain.Business, error)
	// ListMembershipsByUser powers GET /api/v1/businesses.
	ListMembershipsByUser(ctx context.Context, userID uuid.UUID) ([]service.MembershipSummary, error)
	// Tool-approval methods. Permission enforcement (PermBusinessRead /
	// PermBusinessUpdate) is at the handler layer via authz.Can — the
	// service is a thin data wrapper since Phase 6 (CLEAN-01).
	GetToolApprovals(ctx context.Context, businessID uuid.UUID) (map[string]domain.ToolFloor, error)
	UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error
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

// SetToolsCache wires a tools-registry cache for tool-name validation.
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

// createBusinessRequest mirrors UpdateBusinessRequest exactly (BIZ-03 body shape).
type createBusinessRequest struct {
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

// listUserBusinessesResponse is the per-item shape for GET /api/v1/businesses.
type listUserBusinessesResponse struct {
	ID       uuid.UUID              `json:"id"`
	Name     string                 `json:"name"`
	Role     listUserBusinessesRole `json:"role"`
	Status   string                 `json:"status"`
	JoinedAt time.Time              `json:"joined_at"`
}

type listUserBusinessesRole struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ListUserBusinesses handles GET /api/v1/businesses (BIZ-02).
// Returns the businesses the authenticated user is a member of, hydrated
// with business name + role. Auth-only (no BusinessContext needed — the
// user is not yet in a business scope).
func (h *BusinessHandler) ListUserBusinesses(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	memberships, err := h.businessService.ListMembershipsByUser(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "list user businesses failed", "error", err, "user_id", userID)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	out := make([]listUserBusinessesResponse, 0, len(memberships))
	for _, m := range memberships {
		out = append(out, listUserBusinessesResponse{
			ID:   m.BusinessID,
			Name: m.BusinessName,
			Role: listUserBusinessesRole{
				ID:   m.RoleID,
				Name: m.RoleName,
			},
			Status:   m.Status,
			JoinedAt: m.JoinedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateBusiness handles POST /api/v1/businesses (BIZ-03).
// Creates a new business and owner membership for the authenticated user.
func (h *BusinessHandler) CreateBusiness(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createBusinessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	newBusiness := &domain.Business{
		ID:          uuid.New(),
		Name:        req.Name,
		Category:    req.Category,
		Address:     req.Address,
		Phone:       req.Phone,
		Website:     req.Website,
		Description: req.Description,
		Settings:    map[string]interface{}{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	created, err := h.businessService.Create(r.Context(), newBusiness, userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessExists) {
			writeJSONError(w, http.StatusConflict, "business_already_exists")
			return
		}
		slog.ErrorContext(r.Context(), "create business failed", "error", err, "user_id", userID)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// GetBusiness returns the business profile for the request's BusinessContext.
// Requires PermBusinessRead.
func (h *BusinessHandler) GetBusiness(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GetBusiness: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermBusinessRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, business)
}

// UpdateBusiness updates the business profile for the request's BusinessContext.
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateBusiness(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "UpdateBusiness: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermBusinessUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req UpdateBusinessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to get business for update", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	business.Name = req.Name
	business.Category = req.Category
	business.Address = req.Address
	business.Phone = req.Phone
	business.Website = req.Website
	business.Description = req.Description
	business.UpdatedAt = time.Now()

	updatedBusiness, err := h.businessService.Update(r.Context(), business)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to update business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if h.syncer != nil {
		go h.syncer.SyncBusiness(updatedBusiness)
	}

	writeJSON(w, http.StatusOK, updatedBusiness)
}

// UpdateSchedule updates the business schedule (stored in settings).
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "UpdateSchedule: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermBusinessUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Schedule     interface{} `json:"schedule"`
		SpecialDates interface{} `json:"specialDates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if business.Settings == nil {
		business.Settings = make(map[string]interface{})
	}
	business.Settings["schedule"] = req.Schedule
	if req.SpecialDates != nil {
		business.Settings["specialDates"] = req.SpecialDates
	}
	business.UpdatedAt = time.Now()

	updated, err := h.businessService.Update(r.Context(), business)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to update schedule", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if h.syncer != nil {
		go h.syncer.SyncBusiness(updated)
	}

	writeJSON(w, http.StatusOK, updated)
}

// UpdateVoiceTone updates the business voice/tone tags (stored in settings).
// Body: {"tones": ["Тёплый", "Дружеский"]}.
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateVoiceTone(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "UpdateVoiceTone: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermBusinessUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Tones []string `json:"tones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if business.Settings == nil {
		business.Settings = make(map[string]interface{})
	}
	business.Settings["voiceTone"] = req.Tones
	business.UpdatedAt = time.Now()

	updated, err := h.businessService.Update(r.Context(), business)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to update voice tone", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// GetBusinessToolApprovals handles GET /business/{id}/tool-approvals.
// Response shape: `{"toolApprovals": {"tool_name": "auto"|"manual", ...}}`.
// Absence from the map means the registry floor applies.
// Requires PermBusinessRead.
func (h *BusinessHandler) GetBusinessToolApprovals(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GetBusinessToolApprovals: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermBusinessRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	approvals, err := h.businessService.GetToolApprovals(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "get tool approvals failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"toolApprovals": approvals,
	})
}

// updateToolApprovalsRequest is the PUT body shape. Values are strings
// (Auto/Manual); handler converts to ToolFloor after validation.
type updateToolApprovalsRequest struct {
	ToolApprovals map[string]string `json:"toolApprovals"`
}

// UpdateBusinessToolApprovals handles PUT /business/{id}/tool-approvals.
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateBusinessToolApprovals(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "UpdateBusinessToolApprovals: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermBusinessUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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
		if floor != domain.ToolFloorAuto && floor != domain.ToolFloorManual {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid floor for tool " + toolName + ": must be auto or manual",
			})
			return
		}
		approvals[toolName] = floor
	}

	if err := h.businessService.UpdateToolApprovals(r.Context(), bc.BusinessID, approvals); err != nil {
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
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "UploadLogo: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	if !authz.Can(r.Context(), authz.PermBusinessUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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
	ext, ok2 := allowedMimeTypes[mimeType]
	if !ok2 {
		writeJSONError(w, http.StatusBadRequest, "unsupported file type: "+mimeType)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "upload logo: get business failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Cache-bust on re-upload by including UpdatedAt nanos in the key.
	key := fmt.Sprintf("businesses/%s/logo-%d%s", business.ID, time.Now().UnixNano(), ext)
	if err := h.storage.Upload(r.Context(), key, file, header.Size, mimeType); err != nil {
		slog.ErrorContext(r.Context(), "upload logo: storage upload failed", "key", key, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	business.LogoURL = h.storage.PublicURL(key)
	business.UpdatedAt = time.Now()
	updatedBusiness, err := h.businessService.Update(r.Context(), business)
	if err != nil {
		slog.ErrorContext(r.Context(), "upload logo: update business failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if h.syncer != nil {
		go h.syncer.SyncBusiness(updatedBusiness)
	}

	writeJSON(w, http.StatusOK, updatedBusiness)
}
