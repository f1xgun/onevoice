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
	// pendingRepo drives HITL-11's pendingApprovals array on GET /messages.
	// Phase 16 dep.
	pendingRepo domain.PendingToolCallRepository
}

// NewConversationHandler creates a new conversation handler instance.
// businessService and projectService are required (Phase 15) — create-conversation
// and move-conversation must validate that the supplied projectId belongs to the
// caller's business. pendingRepo is required (Phase 16) — GET /messages joins
// the pending_tool_calls collection to hydrate the approval card. Passing nil
// for any dep is a programmer error.
func NewConversationHandler(
	conversationRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
	businessService BusinessService,
	projectService ProjectService,
	pendingRepo domain.PendingToolCallRepository,
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
	if pendingRepo == nil {
		return nil, fmt.Errorf("NewConversationHandler: pendingRepo cannot be nil")
	}
	return &ConversationHandler{
		conversationRepo: conversationRepo,
		messageRepo:      messageRepo,
		businessService:  businessService,
		projectService:   projectService,
		pendingRepo:      pendingRepo,
	}, nil
}

// PendingApprovalSummary is the per-batch projection returned by
// GET /conversations/{id}/messages in the `pendingApprovals` array. Each
// field name matches the JSON contract the Phase 17 frontend consumes to
// render the approval card on page reload (HITL-11).
//
// EditableFields is intentionally left empty in this response: the frontend
// already has the live tool registry via the `['tools']` React Query
// (Plan 16-08 / GET /api/v1/tools), which is the single source of truth for
// per-tool editable-field whitelists. The field is still emitted as [] (not
// omitted) so the JSON schema stays stable for downstream consumers.
type PendingApprovalSummary struct {
	BatchID   string                `json:"batchId"`
	MessageID string                `json:"messageId"`
	Calls     []ApprovalCallSummary `json:"calls"`
	Status    string                `json:"status"`
	CreatedAt time.Time             `json:"createdAt"`
	ExpiresAt time.Time             `json:"expiresAt"`
}

// ApprovalCallSummary mirrors orchestrator.ApprovalCallSummary but scoped to
// the HTTP/JSON contract the frontend consumes. Keeping a local type avoids a
// cross-service import and keeps the api handler decoupled from orchestrator.
type ApprovalCallSummary struct {
	CallID         string                 `json:"callId"`
	ToolName       string                 `json:"toolName"`
	Args           map[string]interface{} `json:"args"`
	EditableFields []string               `json:"editableFields"`
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

	// Create conversation. Phase 19 D-02 — newly created chats start unpinned;
	// PinnedAt stays nil (the single source of truth for the unpinned state).
	// The legacy `Pinned bool` field was removed; do not re-introduce it.
	now := time.Now()
	conversation := &domain.Conversation{
		ID:          primitive.NewObjectID().Hex(),
		UserID:      userID.String(),
		BusinessID:  business.ID.String(),
		ProjectID:   req.ProjectID, // nil → "Без проекта"; both null and absent map here
		Title:       req.Title,
		TitleStatus: domain.TitleStatusAutoPending,
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
	conversation.TitleStatus = domain.TitleStatusManual // Phase 18 / D-06: PUT title is unconditional manual rename. Plan 03's repo Update persists this in $set block.
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

// listMessagesResponse is the JSON shape returned by GET /messages. Messages
// retains the v1.2 wire format; pendingApprovals is the Phase 16 HITL-11
// addition that lets the approval card rehydrate on page reload.
//
// `pendingApprovals` is ALWAYS serialized (even as []) so the frontend can
// iterate unconditionally — never omit or emit null.
type listMessagesResponse struct {
	Messages         []domain.Message         `json:"messages"`
	PendingApprovals []PendingApprovalSummary `json:"pendingApprovals"`
}

// ListMessages handles GET /api/v1/conversations/{id}/messages.
// Phase 16 extends the response with a pendingApprovals array hydrated from
// the pending_tool_calls collection so the frontend approval card (HITL-11)
// can reconstruct its state on page reload.
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
	if messages == nil {
		messages = []domain.Message{}
	}

	// HITL-11: hydrate the approval card from pending_tool_calls. Failure here
	// is non-fatal — the messages list is still useful. The repo performs the
	// lazy-expiration virtualization so any batch past its TTL surfaces as
	// status="expired".
	pendingApprovals := make([]PendingApprovalSummary, 0)
	batches, err := h.pendingRepo.ListPendingByConversation(r.Context(), conversationID)
	if err != nil {
		slog.WarnContext(r.Context(), "list messages: failed to load pending approvals",
			"error", err, "conversation_id", conversationID)
	} else {
		for _, b := range batches {
			summary := PendingApprovalSummary{
				BatchID:   b.ID,
				MessageID: b.MessageID,
				Calls:     make([]ApprovalCallSummary, 0, len(b.Calls)),
				Status:    b.Status,
				CreatedAt: b.CreatedAt,
				ExpiresAt: b.ExpiresAt,
			}
			for _, c := range b.Calls {
				summary.Calls = append(summary.Calls, ApprovalCallSummary{
					CallID:   c.CallID,
					ToolName: c.ToolName,
					Args:     c.Arguments,
					// EditableFields intentionally empty — frontend gets the
					// live whitelist from GET /api/v1/tools (Plan 16-08).
					EditableFields: []string{},
				})
			}
			pendingApprovals = append(pendingApprovals, summary)
		}
	}

	writeJSON(w, http.StatusOK, listMessagesResponse{
		Messages:         messages,
		PendingApprovals: pendingApprovals,
	})
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
