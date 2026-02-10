package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ConversationHandler handles conversation-related HTTP requests
type ConversationHandler struct {
	conversationRepo domain.ConversationRepository
}

// NewConversationHandler creates a new conversation handler instance
func NewConversationHandler(conversationRepo domain.ConversationRepository) *ConversationHandler {
	if conversationRepo == nil {
		panic("conversationRepo cannot be nil")
	}
	return &ConversationHandler{
		conversationRepo: conversationRepo,
	}
}

// CreateConversationRequest represents the conversation creation request
type CreateConversationRequest struct {
	Title string `json:"title" validate:"required,max=200"`
}

// CreateConversation handles POST /api/v1/conversations
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

	// Create conversation
	conversation := &domain.Conversation{
		ID:        primitive.NewObjectID().Hex(),
		UserID:    userID.String(),
		Title:     req.Title,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Save to repository
	if err := h.conversationRepo.Create(r.Context(), conversation); err != nil {
		slog.Error("failed to create conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return conversation with 201 Created
	writeJSON(w, http.StatusCreated, conversation)
}
