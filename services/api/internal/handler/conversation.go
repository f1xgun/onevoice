package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// Constants for conversation pagination
const (
	DefaultConversationLimit = 20
	MaxConversationLimit     = 100
)

// ConversationHandler handles conversation-related HTTP requests
type ConversationHandler struct {
	conversationRepo domain.ConversationRepository
	messageRepo      domain.MessageRepository
	businessService  BusinessService // Phase 15 — resolve caller's business for project scoping
	projectService   ProjectService  // Phase 15 — validate projectId belongs to caller's business
}

// NewConversationHandler creates a new conversation handler instance.
// businessService and projectService are required (Phase 15) — create-conversation
// and move-conversation must validate that the supplied projectId belongs to the
// caller's business. Passing nil for either is a programmer error.
func NewConversationHandler(
	conversationRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
	businessService BusinessService,
	projectService ProjectService,
) (*ConversationHandler, error) {
	if conversationRepo == nil {
		return nil, fmt.Errorf("NewConversationHandler: conversationRepo cannot be nil")
	}
	if messageRepo == nil {
		return nil, fmt.Errorf("NewConversationHandler: messageRepo cannot be nil")
	}
	if businessService == nil {
		return nil, fmt.Errorf("NewConversationHandler: businessService cannot be nil")
	}
	if projectService == nil {
		return nil, fmt.Errorf("NewConversationHandler: projectService cannot be nil")
	}
	return &ConversationHandler{
		conversationRepo: conversationRepo,
		messageRepo:      messageRepo,
		businessService:  businessService,
		projectService:   projectService,
	}, nil
}

// CreateConversationRequest represents the conversation creation request.
// ProjectID is optional: both an explicit JSON `null` and an absent `projectId`
// key map to Go's default `*string = nil` (standard encoding/json semantics).
// Downstream effect in both cases: conversation persisted with project_id = null
// (the "Без проекта" bucket). The handler does NOT distinguish the two cases —
// this is intentional and matches Go's idiomatic JSON handling.
type CreateConversationRequest struct {
	Title     string  `json:"title" validate:"required,max=200"`
	ProjectID *string `json:"projectId"`
}

// CreateConversation handles POST /api/v1/conversations.
//
// Phase 15 (PROJ-05, D-07..D-10): accepts an optional `projectId` in the body.
// Both an explicit JSON `null` and an absent `projectId` key deserialize to
// Go's `*string = nil` — both cases persist `project_id: null` (the "Без проекта"
// bucket). When projectId is non-empty, the handler validates that the project
// exists AND belongs to the caller's business before creating the conversation;
// cross-business or missing project returns 404.
func (h *ConversationHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse request body
	var req CreateConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate request
	if err := validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	// Resolve caller's business — needed for scoping AND for validating the
	// supplied projectId (if any).
	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "create conversation: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// If a projectId was supplied, validate it exists and belongs to the
	// caller's business. Cross-business or missing project → 404 (per
	// docs/security.md we do NOT leak existence via 403).
	if req.ProjectID != nil && *req.ProjectID != "" {
		projUUID, parseErr := uuid.Parse(*req.ProjectID)
		if parseErr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid project id")
			return
		}
		if _, projErr := h.projectService.GetByID(r.Context(), business.ID, projUUID); projErr != nil {
			if errors.Is(projErr, domain.ErrProjectNotFound) {
				writeJSONError(w, http.StatusNotFound, "project not found")
				return
			}
			slog.ErrorContext(r.Context(), "create conversation: failed to resolve project", "error", projErr)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	// Create conversation
	now := time.Now()
	conversation := &domain.Conversation{
		ID:          primitive.NewObjectID().Hex(),
		UserID:      userID.String(),
		BusinessID:  business.ID.String(),
		ProjectID:   req.ProjectID, // nil → "Без проекта"; both null and absent map here
		Title:       req.Title,
		TitleStatus: domain.TitleStatusAutoPending,
		Pinned:      false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Save to repository
	if err := h.conversationRepo.Create(r.Context(), conversation); err != nil {
		slog.ErrorContext(r.Context(), "failed to create conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return conversation with 201 Created
	writeJSON(w, http.StatusCreated, conversation)
}

// ListConversations handles GET /api/v1/conversations
func (h *ConversationHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query parameters
	limit := DefaultConversationLimit
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			// Enforce max limit
			if limit > MaxConversationLimit {
				limit = MaxConversationLimit
			}
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	// Get conversations from repository
	conversations, err := h.conversationRepo.ListByUserID(r.Context(), userID.String(), limit, offset)
	if err != nil {
		slog.Error("failed to list conversations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return conversations array (empty array if none)
	writeJSON(w, http.StatusOK, conversations)
}

// GetConversation handles GET /api/v1/conversations/{id}
func (h *ConversationHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Extract conversation ID from URL path
	conversationID := chi.URLParam(r, "id")

	// Validate ObjectID format (MongoDB ObjectID is 24 hex characters)
	if len(conversationID) != 24 {
		writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}
	if _, err := primitive.ObjectIDFromHex(conversationID); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}

	// Get conversation from repository
	conversation, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.Error("failed to get conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Authorization check: verify conversation belongs to user
	if conversation.UserID != userID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Return conversation
	writeJSON(w, http.StatusOK, conversation)
}

// UpdateConversationRequest represents the conversation update request
type UpdateConversationRequest struct {
	Title string `json:"title" validate:"required,max=200"`
}

// UpdateConversation handles PUT /api/v1/conversations/{id}
func (h *ConversationHandler) UpdateConversation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "id")

	var req UpdateConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	conversation, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.Error("failed to get conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if conversation.UserID != userID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	conversation.Title = req.Title
	if err := h.conversationRepo.Update(r.Context(), conversation); err != nil {
		slog.Error("failed to update conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, conversation)
}

// DeleteConversation handles DELETE /api/v1/conversations/{id}
func (h *ConversationHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "id")

	conversation, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.Error("failed to get conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if conversation.UserID != userID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.conversationRepo.Delete(r.Context(), conversationID); err != nil {
		slog.Error("failed to delete conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListMessages handles GET /api/v1/conversations/{id}/messages
func (h *ConversationHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "id")

	// Verify conversation exists and belongs to user
	conversation, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.Error("failed to get conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if conversation.UserID != userID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	messages, err := h.messageRepo.ListByConversationID(r.Context(), conversationID, 200, 0)
	if err != nil {
		slog.Error("failed to list messages", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, messages)
}

// MoveConversationRequest is the body for POST /api/v1/conversations/{id}/move.
// ProjectID may be an explicit JSON null — the two are treated identically
// (standard encoding/json semantics). Null / empty / absent all move the chat
// into the virtual "Без проекта" bucket (D-10).
type MoveConversationRequest struct {
	ProjectID *string `json:"projectId"`
}

// MoveConversation handles POST /api/v1/conversations/{id}/move.
//
// Phase 15 (PROJ-06, D-11..D-13). The endpoint:
//  1. Validates the caller owns the conversation.
//  2. If a destination projectId is supplied, validates it belongs to the
//     caller's business (cross-business → 404 to avoid enumeration).
//  3. Atomically updates `project_id` via UpdateProjectAssignment.
//  4. Appends a visible system-role message to the chat documenting the move
//     so the LLM sees the transition on the NEXT turn (PITFALLS §11, Option A).
//     Copy is byte-exact per 15-UI-SPEC line 194:
//     "[Чат перемещён в «{destination}» — с этого момента применяется новая политика]"
//     where {destination} is the new project's name or the literal string
//     "Без проекта" for null moves.
//  5. Returns the updated conversation (re-fetched after the update).
//
// The system-note append is best-effort: if messageRepo.Create fails, the move
// itself already landed, so we log and still return success. Rolling back the
// move on a note-append failure would be more surprising than a missing note.
func (h *ConversationHandler) MoveConversation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "id")

	var req MoveConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	conv, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.ErrorContext(r.Context(), "move conversation: get conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if conv.UserID != userID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "move conversation: get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Resolve destination name for the system note. Null / empty / absent
	// projectId all map to "Без проекта".
	destName := "Без проекта"
	if req.ProjectID != nil && *req.ProjectID != "" {
		projUUID, parseErr := uuid.Parse(*req.ProjectID)
		if parseErr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid project id")
			return
		}
		proj, projErr := h.projectService.GetByID(r.Context(), business.ID, projUUID)
		if projErr != nil {
			if errors.Is(projErr, domain.ErrProjectNotFound) {
				writeJSONError(w, http.StatusNotFound, "project not found")
				return
			}
			slog.ErrorContext(r.Context(), "move conversation: get project", "error", projErr)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		destName = proj.Name
	}

	if err := h.conversationRepo.UpdateProjectAssignment(r.Context(), conversationID, req.ProjectID); err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.ErrorContext(r.Context(), "move conversation: update project assignment", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Append the visible system note (D-13; byte-exact Russian copy per
	// 15-UI-SPEC line 194). The LLM sees this on the NEXT turn of the chat
	// so the prompt-layering transition is explicit (PITFALLS §11 Option A).
	note := &domain.Message{
		ConversationID: conversationID,
		Role:           "system",
		Content:        fmt.Sprintf("[Чат перемещён в «%s» — с этого момента применяется новая политика]", destName),
		CreatedAt:      time.Now(),
	}
	if err := h.messageRepo.Create(r.Context(), note); err != nil {
		// Best-effort — the move itself already landed; log but don't fail.
		slog.ErrorContext(r.Context(), "move conversation: failed to append system note", "error", err)
	}

	// Re-fetch to return the current state including the new project_id.
	updated, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		slog.ErrorContext(r.Context(), "move conversation: refetch conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
