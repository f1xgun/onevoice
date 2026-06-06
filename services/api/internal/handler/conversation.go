package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// Pagination + ObjectID-length constants. See docs/api/handlers/conversation.md.
const (
	DefaultConversationLimit = 20
	MaxConversationLimit     = 100
	mongoObjectIDHexLen      = 24
)

// ConversationHandler serves the conversation CRUD + move/pin/unpin/list-messages surface.
// See docs/api/handlers/conversation.md.
type ConversationHandler struct {
	conversationRepo    domain.ConversationRepository
	messageRepo         domain.MessageRepository
	businessService     BusinessService
	projectService      ProjectService
	conversationService ConversationService
}

// NewConversationHandler constructs a ConversationHandler; all five deps are required.
func NewConversationHandler(
	conversationRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
	businessService BusinessService,
	projectService ProjectService,
	conversationService ConversationService,
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
	if conversationService == nil {
		return nil, fmt.Errorf("NewConversationHandler: conversationService cannot be nil")
	}
	return &ConversationHandler{
		conversationRepo:    conversationRepo,
		messageRepo:         messageRepo,
		businessService:     businessService,
		projectService:      projectService,
		conversationService: conversationService,
	}, nil
}

// CreateConversation handles POST /conversations. See docs/api/handlers/conversation.md.
func (h *ConversationHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "CreateConversation", authz.PermContentCreate)
	if !ok {
		return
	}

	req, ok := decodeAndValidate[openapi.CreateConversationRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	if req.ProjectId != nil && *req.ProjectId != "" {
		projUUID, parseErr := uuid.Parse(*req.ProjectId)
		if parseErr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid project id")
			return
		}
		if _, projErr := h.projectService.GetByID(r.Context(), bc.BusinessID, projUUID); projErr != nil {
			if errors.Is(projErr, domain.ErrProjectNotFound) {
				writeJSONError(w, http.StatusNotFound, "project not found")
				return
			}
			slog.ErrorContext(r.Context(), "create conversation: failed to resolve project", "error", projErr)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	now := time.Now()
	conversation := &domain.Conversation{
		ID:          primitive.NewObjectID().Hex(),
		UserID:      bc.UserID.String(),
		BusinessID:  bc.BusinessID.String(),
		ProjectID:   req.ProjectId,
		Title:       req.Title,
		TitleStatus: domain.TitleStatusAutoPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.conversationRepo.Create(r.Context(), conversation); err != nil {
		slog.ErrorContext(r.Context(), "failed to create conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, conversation)
}

// ListConversations handles GET /conversations. See docs/api/handlers/conversation.md.
func (h *ConversationHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ListConversations", authz.PermContentRead)
	if !ok {
		return
	}

	limit, offset := parseLimitOffset(r, DefaultConversationLimit, MaxConversationLimit)

	conversations, err := h.conversationRepo.ListByUserID(r.Context(), bc.UserID.String(), limit, offset)
	if err != nil {
		slog.Error("failed to list conversations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

// GetConversation handles GET /conversations/{id}. See docs/api/handlers/conversation.md.
func (h *ConversationHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetConversation", authz.PermContentRead)
	if !ok {
		return
	}

	conversationID := chi.URLParam(r, "id")
	if len(conversationID) != mongoObjectIDHexLen {
		writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}
	if _, err := primitive.ObjectIDFromHex(conversationID); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
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

	if conversation.UserID != bc.UserID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	writeJSON(w, http.StatusOK, conversation)
}

// UpdateConversation handles PUT /conversations/{id} — manual rename.
// See docs/api/handlers/conversation.md §"UpdateConversation".
func (h *ConversationHandler) UpdateConversation(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "UpdateConversation", authz.PermContentUpdate)
	if !ok {
		return
	}

	conversationID := chi.URLParam(r, "id")

	req, ok := decodeAndValidate[openapi.UpdateConversationRequest](w, r, "invalid request body")
	if !ok {
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
	if conversation.UserID != bc.UserID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	conversation.Title = req.Title
	conversation.TitleStatus = domain.TitleStatusManual
	if err := h.conversationRepo.Update(r.Context(), conversation); err != nil {
		slog.Error("failed to update conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, conversation)
}

// DeleteConversation handles DELETE /conversations/{id}.
// See docs/api/handlers/conversation.md.
func (h *ConversationHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "DeleteConversation", authz.PermContentDelete)
	if !ok {
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
	if conversation.UserID != bc.UserID.String() {
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

// ListMessages handles GET /conversations/{id}/messages — adapter over
// ConversationService.OpenChat. See docs/api/handlers/conversation.md.
func (h *ConversationHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ListMessages", authz.PermContentRead)
	if !ok {
		return
	}

	conversationID := chi.URLParam(r, "id")

	view, err := h.conversationService.OpenChat(r.Context(), conversationID, bc.UserID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrConversationNotFound):
			writeJSONError(w, http.StatusNotFound, "conversation not found")
		case errors.Is(err, domain.ErrForbidden):
			writeJSONError(w, http.StatusForbidden, "forbidden")
		default:
			slog.ErrorContext(r.Context(), "list messages: service error", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// MoveConversation handles POST /conversations/{id}/move. See docs/api/handlers/conversation.md.
// Null/empty/absent ProjectId all map to the "Без проекта" bucket.
func (h *ConversationHandler) MoveConversation(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "MoveConversation", authz.PermContentUpdate)
	if !ok {
		return
	}

	conversationID := chi.URLParam(r, "id")

	var req openapi.MoveConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.conversationService.MoveToProject(
		r.Context(), conversationID, bc.BusinessID, bc.UserID, req.ProjectId)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrConversationNotFound):
			writeJSONError(w, http.StatusNotFound, "conversation not found")
		case errors.Is(err, domain.ErrForbidden):
			writeJSONError(w, http.StatusForbidden, "forbidden")
		case errors.Is(err, domain.ErrProjectNotFound):
			writeJSONError(w, http.StatusNotFound, "project not found")
		case errors.Is(err, service.ErrInvalidProjectID):
			writeJSONError(w, http.StatusBadRequest, "invalid project id")
		default:
			slog.ErrorContext(r.Context(), "move conversation: service error", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// Pin handles POST /conversations/{id}/pin — atomic pinned_at=now scoped
// by (id, business_id, user_id). Cross-tenant → uniform 404 (no leak).
// See docs/api/handlers/conversation.md §"Pin / unpin atomicity".
func (h *ConversationHandler) Pin(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "Pin", authz.PermContentUpdate)
	if !ok {
		return
	}

	conversationID := chi.URLParam(r, "id")
	if len(conversationID) != mongoObjectIDHexLen {
		writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}
	if _, err := primitive.ObjectIDFromHex(conversationID); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}

	if err := h.conversationRepo.Pin(r.Context(), conversationID, bc.BusinessID.String(), bc.UserID.String()); err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.ErrorContext(r.Context(), "pin conversation failed",
			"conversation_id", conversationID,
			"user_id", bc.UserID.String(),
			"business_id", bc.BusinessID.String(),
			"error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	conv, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		slog.ErrorContext(r.Context(), "pin conversation: refetch failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

// Unpin handles POST /conversations/{id}/unpin — symmetric to Pin.
// See docs/api/handlers/conversation.md §"Pin / unpin atomicity".
func (h *ConversationHandler) Unpin(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "Unpin", authz.PermContentUpdate)
	if !ok {
		return
	}

	conversationID := chi.URLParam(r, "id")
	if len(conversationID) != mongoObjectIDHexLen {
		writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}
	if _, err := primitive.ObjectIDFromHex(conversationID); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}

	if err := h.conversationRepo.Unpin(r.Context(), conversationID, bc.BusinessID.String(), bc.UserID.String()); err != nil {
		if errors.Is(err, domain.ErrConversationNotFound) {
			writeJSONError(w, http.StatusNotFound, "conversation not found")
			return
		}
		slog.ErrorContext(r.Context(), "unpin conversation failed",
			"conversation_id", conversationID,
			"user_id", bc.UserID.String(),
			"business_id", bc.BusinessID.String(),
			"error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	conv, err := h.conversationRepo.GetByID(r.Context(), conversationID)
	if err != nil {
		slog.ErrorContext(r.Context(), "unpin conversation: refetch failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, conv)
}
