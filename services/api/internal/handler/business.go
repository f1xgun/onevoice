package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
)

const maxUploadSize = 5 << 20 // 5 MB

// maxBusinessBodyBytes caps the JSON request body on the business write
// endpoints (profile, schedule, voice tone). The profile fields are bounded
// individually by the max= validators; this guards the decoder itself and the
// free-form JSONB blobs from an unbounded body.
const maxBusinessBodyBytes = 64 * 1024

// maxSettingsBlobBytes caps the marshaled size of each free-form value stored
// into the JSONB settings column (schedule, special dates, voice tones) so a
// single business row cannot be bloated by an arbitrarily large blob.
const maxSettingsBlobBytes = 16 * 1024

// maxReviewRating is the inclusive upper bound of a review star rating; it caps
// the review-autopilot minRating so a stored floor can never exceed 5 stars.
const maxReviewRating = 5

var allowedMimeTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// settingsBlobWithinCap marshals v and rejects (400) when the serialized form
// exceeds maxSettingsBlobBytes, guarding the free-form JSONB settings column
// from arbitrarily large blobs. It returns false (after writing the error)
// when the blob is too large or unmarshalable.
func settingsBlobWithinCap(w http.ResponseWriter, v interface{}, errMsg string) bool {
	blob, err := json.Marshal(v)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if len(blob) > maxSettingsBlobBytes {
		writeJSONError(w, http.StatusBadRequest, errMsg)
		return false
	}
	return true
}

// BusinessService defines the interface for business operations
type BusinessService interface {
	Create(ctx context.Context, business *domain.Business, ownerUserID uuid.UUID) (*domain.Business, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	// Update applies a business profile edit; actorUserID is threaded through
	// for the service-layer audit emission. It does not touch logo_url.
	Update(ctx context.Context, business *domain.Business, actorUserID uuid.UUID) (*domain.Business, error)
	// UpdateLogoURL writes only the logo_url via a targeted column update and
	// returns the re-read row, so a concurrent profile edit cannot revert the
	// new logo (and vice versa).
	UpdateLogoURL(ctx context.Context, businessID uuid.UUID, url string, actorUserID uuid.UUID) (*domain.Business, error)
	// UpdateSettingsKeys writes only the named settings sub-keys (schedule,
	// voiceTone, specialDates) via a targeted jsonb_set and returns the re-read
	// row. Sibling settings sub-keys (incl. tool_approvals) are preserved so a
	// concurrent writer of a different sub-key is never reverted.
	UpdateSettingsKeys(ctx context.Context, businessID uuid.UUID, keys map[string]interface{}, actorUserID uuid.UUID) (*domain.Business, error)
	// ListMembershipsByUser powers GET /api/v1/businesses.
	ListMembershipsByUser(ctx context.Context, userID uuid.UUID) ([]service.MembershipSummary, error)
	// Tool-approval methods. Permission enforcement (PermBusinessRead /
	// PermBusinessUpdate) is at the handler layer via authz.Can — the
	// service is a thin data wrapper since (CLEAN-01).
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

// domainMembershipToOpenAPI maps a service-layer MembershipSummary to the
// spec-side BusinessMembershipSummary used as the wire shape for
// GET /api/v1/businesses.
func domainMembershipToOpenAPI(m service.MembershipSummary) openapi.BusinessMembershipSummary {
	return openapi.BusinessMembershipSummary{
		Id:   m.BusinessID,
		Name: m.BusinessName,
		Role: openapi.BusinessMembershipRoleRef{
			Id:   m.RoleID,
			Name: m.RoleName,
		},
		Status:   m.Status,
		JoinedAt: m.JoinedAt,
	}
}

// ListUserBusinesses handles GET /api/v1/businesses (BIZ-02).
// Returns the businesses the authenticated user is a member of, hydrated
// with business name + role. Auth-only (no BusinessContext needed — the
// user is not yet in a business scope).
func (h *BusinessHandler) ListUserBusinesses(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	memberships, err := h.businessService.ListMembershipsByUser(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "list user businesses failed", "error", err, "user_id", userID)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	out := make([]openapi.BusinessMembershipSummary, 0, len(memberships))
	for _, m := range memberships {
		out = append(out, domainMembershipToOpenAPI(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateBusiness handles POST /api/v1/businesses (BIZ-03).
// Creates a new business and owner membership for the authenticated user.
func (h *BusinessHandler) CreateBusiness(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	req, ok := decodeAndValidate[openapi.CreateBusinessRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	newBusiness := &domain.Business{
		ID:          uuid.New(),
		Name:        req.Name,
		Category:    strDeref(req.Category),
		Address:     strDeref(req.Address),
		Phone:       strDeref(req.Phone),
		Website:     req.Website,
		Description: strDeref(req.Description),
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
	bc, ok := requireBusiness(w, r, "GetBusiness", authz.PermBusinessRead)
	if !ok {
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
	bc, ok := requireBusiness(w, r, "UpdateBusiness", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	req, ok := decodeAndValidate[openapi.UpdateBusinessRequest](w, r, "invalid request body")
	if !ok {
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
	business.Category = strDeref(req.Category)
	business.Address = strDeref(req.Address)
	business.Phone = strDeref(req.Phone)
	business.Website = req.Website
	business.Description = strDeref(req.Description)
	business.UpdatedAt = time.Now()

	updatedBusiness, err := h.businessService.Update(r.Context(), business, bc.UserID)
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
	bc, ok := requireBusiness(w, r, "UpdateSchedule", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	var req openapi.UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var schedule interface{}
	if req.Schedule != nil {
		schedule = *req.Schedule
	}
	if !settingsBlobWithinCap(w, schedule, "schedule too large") {
		return
	}
	keys := map[string]interface{}{"schedule": schedule}
	if req.SpecialDates != nil {
		if !settingsBlobWithinCap(w, *req.SpecialDates, "special dates too large") {
			return
		}
		keys["specialDates"] = *req.SpecialDates
	}

	updated, err := h.businessService.UpdateSettingsKeys(r.Context(), bc.BusinessID, keys, bc.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
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
	bc, ok := requireBusiness(w, r, "UpdateVoiceTone", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	var req openapi.UpdateVoiceToneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var tones []string
	if req.Tones != nil {
		tones = *req.Tones
	}
	if !settingsBlobWithinCap(w, tones, "voice tone too large") {
		return
	}

	updated, err := h.businessService.UpdateSettingsKeys(r.Context(), bc.BusinessID, map[string]interface{}{"voiceTone": tones}, bc.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to update voice tone", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// GetDescriptionTemplate handles GET /business/{id}/description-template.
// Returns the stored template override (empty string when unset) plus the
// allowed placeholder names for the editor.
// Requires PermBusinessRead.
func (h *BusinessHandler) GetDescriptionTemplate(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetDescriptionTemplate", authz.PermBusinessRead)
	if !ok {
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "get description template failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	tmpl := ""
	if business.Settings != nil {
		if v, ok := business.Settings[platform.DescriptionTemplateSettingsKey].(string); ok {
			tmpl = v
		}
	}

	writeJSON(w, http.StatusOK, openapi.DescriptionTemplateResponse{
		DescriptionTemplate: tmpl,
		Placeholders:        platform.AllowedDescriptionPlaceholders,
	})
}

// UpdateDescriptionTemplate handles PUT /business/{id}/description-template.
// A non-empty template is a full replacement of the platform description; an
// empty string clears the override. Unknown placeholder tokens are rejected.
// On success the change is fanned out to connected platforms.
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateDescriptionTemplate(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "UpdateDescriptionTemplate", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	req, ok := decodeAndValidate[openapi.UpdateDescriptionTemplateRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	tmpl := strDeref(req.DescriptionTemplate)
	if unknown := platform.UnknownDescriptionPlaceholders(tmpl); len(unknown) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "unknown placeholder(s): " + strings.Join(unknown, ", "),
		})
		return
	}
	if !settingsBlobWithinCap(w, tmpl, "description template too large") {
		return
	}

	updated, err := h.businessService.UpdateSettingsKeys(r.Context(), bc.BusinessID, map[string]interface{}{platform.DescriptionTemplateSettingsKey: tmpl}, bc.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to update description template", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if h.syncer != nil {
		go h.syncer.SyncBusiness(updated)
	}

	writeJSON(w, http.StatusOK, updated)
}

// GetVoiceProfile handles GET /business/{id}/voice-profile.
// Returns the stored brand-voice profile (empty string when unset).
// Requires PermBusinessRead.
func (h *BusinessHandler) GetVoiceProfile(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetVoiceProfile", authz.PermBusinessRead)
	if !ok {
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "get voice profile failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, openapi.VoiceProfileResponse{
		VoiceProfile: platform.VoiceProfileFromSettings(business.Settings),
	})
}

// UpdateVoiceProfile handles PUT /business/{id}/voice-profile.
// A non-empty value is stored verbatim and governs both the chat loop and the
// review drafter; an empty string clears the override. Unlike the description
// template it is not fanned out to any platform — it changes prompt text only,
// not a platform-visible field.
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateVoiceProfile(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "UpdateVoiceProfile", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	req, ok := decodeAndValidate[openapi.UpdateVoiceProfileRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	profile := strDeref(req.VoiceProfile)
	if !settingsBlobWithinCap(w, profile, "voice profile too large") {
		return
	}

	updated, err := h.businessService.UpdateSettingsKeys(r.Context(), bc.BusinessID, map[string]interface{}{platform.VoiceProfileSettingsKey: profile}, bc.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to update voice profile", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// GetReviewAutopilot handles GET /business/{id}/review-autopilot.
// Returns the stored review-reply autopilot config. When unset the response is
// the default-off state {enabled:false, minRating:ReviewAutopilotMinRatingFloor}.
// Requires PermBusinessRead.
func (h *BusinessHandler) GetReviewAutopilot(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetReviewAutopilot", authz.PermBusinessRead)
	if !ok {
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "get review autopilot failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	cfg := platform.ReviewAutopilotFromSettings(business.Settings)
	writeJSON(w, http.StatusOK, openapi.ReviewAutopilotResponse{
		Enabled:   cfg.Enabled,
		MinRating: cfg.MinRating,
	})
}

// UpdateReviewAutopilot handles PUT /business/{id}/review-autopilot.
// Stores the opt-in autopilot config. minRating is bounded to the positive range
// [ReviewAutopilotMinRatingFloor..5] so the setting can only RAISE the positive
// floor and can never be configured to auto-publish a negative or neutral review.
// Like the voice profile it is prompt/automation-only and is NOT fanned out to
// any connected platform. Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateReviewAutopilot(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "UpdateReviewAutopilot", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	req, ok := decodeAndValidate[openapi.UpdateReviewAutopilotRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	if req.MinRating < platform.ReviewAutopilotMinRatingFloor || req.MinRating > maxReviewRating {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("minRating must be between %d and %d", platform.ReviewAutopilotMinRatingFloor, maxReviewRating),
		})
		return
	}

	cfg := platform.ReviewAutopilotConfig{Enabled: boolDeref(req.Enabled), MinRating: req.MinRating}
	updated, err := h.businessService.UpdateSettingsKeys(r.Context(), bc.BusinessID, map[string]interface{}{platform.ReviewAutopilotSettingsKey: cfg}, bc.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to update review autopilot", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// GetOwnerBrief handles GET /businesses/{id}/owner-brief.
// Returns the stored weekly-owner-brief preferences. Default-on: a business that
// never set a preference reads back enabled=true, Monday 09:00.
// Requires PermBusinessRead.
func (h *BusinessHandler) GetOwnerBrief(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetOwnerBrief", authz.PermBusinessRead)
	if !ok {
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "get owner brief failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	pref := platform.OwnerBriefFromSettings(business.Settings)
	resp := openapi.OwnerBriefResponse{
		Enabled: pref.Enabled,
		Weekday: pref.Weekday,
		Hour:    pref.Hour,
	}
	if lastSent := platform.OwnerBriefLastSentFromSettings(business.Settings); lastSent != "" {
		resp.LastSent = &lastSent
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateOwnerBrief handles PUT /businesses/{id}/owner-brief.
// Enables/disables the weekly owner-brief DM (enabled=false is the one-tap
// opt-out) and optionally sets the weekday/hour window. Absent fields keep their
// current stored value so a caller can toggle enabled without resupplying the
// schedule. Written via the same jsonb_set sub-key path as voiceProfile (no
// migration). Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateOwnerBrief(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "UpdateOwnerBrief", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	req, ok := decodeAndValidate[openapi.UpdateOwnerBriefRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "update owner brief: load failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	pref := platform.OwnerBriefFromSettings(business.Settings)
	if req.Enabled != nil {
		pref.Enabled = *req.Enabled
	}
	if req.Weekday != nil {
		pref.Weekday = *req.Weekday
	}
	if req.Hour != nil {
		pref.Hour = *req.Hour
	}

	value := map[string]interface{}{
		"enabled": pref.Enabled,
		"weekday": pref.Weekday,
		"hour":    pref.Hour,
	}
	updated, err := h.businessService.UpdateSettingsKeys(r.Context(), bc.BusinessID, map[string]interface{}{platform.OwnerBriefSettingsKey: value}, bc.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to update owner brief", "error", err)
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
	bc, ok := requireBusiness(w, r, "GetBusinessToolApprovals", authz.PermBusinessRead)
	if !ok {
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

// UpdateBusinessToolApprovals handles PUT /business/{id}/tool-approvals.
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateBusinessToolApprovals(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "UpdateBusinessToolApprovals", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	if h.toolsCache == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "tool registry unavailable")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	var req openapi.UpdateToolApprovalsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	approvals := make(map[string]domain.ToolFloor, len(req.ToolApprovals))
	for toolName, floor := range req.ToolApprovals {
		if !h.toolsCache.Has(toolName) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "unknown tool: " + toolName,
			})
			return
		}
		df := domain.ToolFloor(floor)
		if df != domain.ToolFloorAuto && df != domain.ToolFloorManual {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid floor for tool " + toolName + ": must be auto or manual",
			})
			return
		}
		approvals[toolName] = df
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
	bc, ok := requireBusiness(w, r, "UploadLogo", authz.PermBusinessUpdate)
	if !ok {
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

	key := fmt.Sprintf("businesses/%s/logo-%d%s", business.ID, time.Now().UnixNano(), ext)
	if err := h.storage.Upload(r.Context(), key, file, header.Size, mimeType); err != nil {
		slog.ErrorContext(r.Context(), "upload logo: storage upload failed", "key", key, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	priorLogoURL := business.LogoURL
	newLogoURL := h.storage.PublicURL(key)
	updatedBusiness, err := h.businessService.UpdateLogoURL(r.Context(), bc.BusinessID, newLogoURL, bc.UserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "upload logo: update business failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if priorKey := h.storage.KeyFromPublicURL(priorLogoURL); priorKey != "" && priorKey != key {
		if delErr := h.storage.Delete(r.Context(), priorKey); delErr != nil {
			slog.WarnContext(r.Context(), "upload logo: delete prior object failed (non-fatal)", "key", priorKey, "error", delErr)
		}
	}

	if h.syncer != nil {
		go h.syncer.SyncBusiness(updatedBusiness)
	}

	writeJSON(w, http.StatusOK, updatedBusiness)
}
