package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Constants for conversation pagination
const (
	DefaultConversationLimit = 20
	MaxConversationLimit     = 100
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
