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

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
)

// Constants for conversation pagination
const (
	DefaultConversationLimit = 20
	MaxConversationLimit     = 100
)

// Mongo / handler-side limits.
const (
	// mongoObjectIDHexLen is the length of a hex-encoded Mongo ObjectID.
	mongoObjectIDHexLen = 24

	// defaultMessageListLimit caps the number of messages returned by
	// GET /conversations/:id/messages. The frontend chat history view
	// renders the latest N; older entries require explicit pagination.
	defaultMessageListLimit = 200
)

// ConversationHandler handles conversation-related HTTP requests
type ConversationHandler struct {
	conversationRepo domain.ConversationRepository
	messageRepo      domain.MessageRepository
	// businessService retained for wiring compatibility; not called by handlers
	// after RBAC refactor — BusinessContext from RequireBusinessAccess
	// middleware provides businessID + userID directly.
	businessService BusinessService
	// projectService validates projectId belongs to caller's business.
	projectService ProjectService
	// pendingRepo drives the pendingApprovals array on GET /messages.
	pendingRepo domain.PendingToolCallRepository
}

// NewConversationHandler creates a new conversation handler instance.
// businessService and projectService are required — create-conversation
// and move-conversation must validate that the supplied projectId belongs to the
// caller's business. pendingRepo is required — GET /messages joins
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
// field name matches the JSON contract the frontend consumes to
// render the approval card on page reload.
//
// EditableFields is intentionally left empty in this response: the frontend
// already has the live tool registry via the `['tools']` React Query
// (GET /api/v1/tools), which is the single source of truth for
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
// Accepts an optional `projectId` in the body.
// Both an explicit JSON `null` and an absent `projectId` key deserialize to
// Go's `*string = nil` — both cases persist `project_id: null` (the "Без проекта"
// bucket). When projectId is non-empty, the handler validates that the project
// exists AND belongs to the caller's business before creating the conversation;
// cross-business or missing project returns 404.
func (h *ConversationHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "CreateConversation: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentCreate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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
		writeValidationError(w, r, err)
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

	// Create conversation. Newly created chats start unpinned;
	// PinnedAt stays nil (the single source of truth for the unpinned state).
	// The legacy `Pinned bool` field was removed; do not re-introduce it.
	now := time.Now()
	conversation := &domain.Conversation{
		ID:          primitive.NewObjectID().Hex(),
		UserID:      bc.UserID.String(),
		BusinessID:  bc.BusinessID.String(),
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
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ListConversations: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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
	conversations, err := h.conversationRepo.ListByUserID(r.Context(), bc.UserID.String(), limit, offset)
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
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GetConversation: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Extract conversation ID from URL path
	conversationID := chi.URLParam(r, "id")

	// Validate ObjectID format (MongoDB ObjectID is 24 hex characters)
	if len(conversationID) != mongoObjectIDHexLen {
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
	if conversation.UserID != bc.UserID.String() {
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
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "UpdateConversation: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	conversationID := chi.URLParam(r, "id")

	var req UpdateConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
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
	conversation.TitleStatus = domain.TitleStatusManual // PUT title is unconditional manual rename. The repo Update persists this in $set block.
	if err := h.conversationRepo.Update(r.Context(), conversation); err != nil {
		slog.Error("failed to update conversation", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, conversation)
}

// DeleteConversation handles DELETE /api/v1/conversations/{id}
func (h *ConversationHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "DeleteConversation: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentDelete) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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

// listMessagesResponse is the JSON shape returned by GET /messages. Messages
// retains the v1.2 wire format; pendingApprovals lets the approval card
// rehydrate on page reload.
//
// `pendingApprovals` is ALWAYS serialized (even as []) so the frontend can
// iterate unconditionally — never omit or emit null.
type listMessagesResponse struct {
	Messages         []domain.Message         `json:"messages"`
	PendingApprovals []PendingApprovalSummary `json:"pendingApprovals"`
}

// ListMessages handles GET /api/v1/conversations/{id}/messages.
// Extends the response with a pendingApprovals array hydrated from
// the pending_tool_calls collection so the frontend approval card can
// reconstruct its state on page reload.
func (h *ConversationHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ListMessages: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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
	if conversation.UserID != bc.UserID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	messages, err := h.messageRepo.ListByConversationID(r.Context(), conversationID, defaultMessageListLimit, 0)
	if err != nil {
		slog.Error("failed to list messages", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if messages == nil {
		messages = []domain.Message{}
	}

	// Hydrate the approval card from pending_tool_calls. Failure here
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
					// live whitelist from GET /api/v1/tools.
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
// into the virtual "Без проекта" bucket.
type MoveConversationRequest struct {
	ProjectID *string `json:"projectId"`
}

// MoveConversation handles POST /api/v1/conversations/{id}/move.
//
// The endpoint:
//  1. Validates the caller owns the conversation.
//  2. If a destination projectId is supplied, validates it belongs to the
//     caller's business (cross-business → 404 to avoid enumeration).
//  3. Atomically updates `project_id` via UpdateProjectAssignment.
//  4. Appends a visible system-role message to the chat documenting the move
//     so the LLM sees the transition on the NEXT turn (PITFALLS §11, Option A).
//     Copy is byte-exact:
//     "[Чат перемещён в «{destination}» — с этого момента применяется новая политика]"
//     where {destination} is the new project's name or the literal string
//     "Без проекта" for null moves.
//  5. Returns the updated conversation (re-fetched after the update).
//
// The system-note append is best-effort: if messageRepo.Create fails, the move
// itself already landed, so we log and still return success. Rolling back the
// move on a note-append failure would be more surprising than a missing note.
func (h *ConversationHandler) MoveConversation(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "MoveConversation: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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
	if conv.UserID != bc.UserID.String() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Resolve destination name for the system note. Null / empty / absent
	// projectId all map to the localized "no project" bucket label
	// (RU: "Без проекта", EN: "No project"). The label is interpolated into
	// the system note %s; persisted at write-time per the writer's locale
	// — historic messages keep the language they were created in.
	destName := i18n.Tr(r.Context(), "api.conversation.move.default_destination")
	if req.ProjectID != nil && *req.ProjectID != "" {
		projUUID, parseErr := uuid.Parse(*req.ProjectID)
		if parseErr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid project id")
			return
		}
		proj, projErr := h.projectService.GetByID(r.Context(), bc.BusinessID, projUUID)
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

	// Append the visible system note, localized at write-time using the
	// writer's Accept-Language (planted on r.Context() by middleware.Locale).
	// The LLM sees this on the NEXT turn of the chat so the prompt-layering
	// transition is explicit (PITFALLS §11 Option A). Persisted to MongoDB —
	// historic messages keep the language they were created in (we do NOT
	// retroactively re-translate).
	note := &domain.Message{
		ConversationID: conversationID,
		Role:           "system",
		Content:        i18n.Tr(r.Context(), "api.conversation.move.system_message", destName),
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

// Pin handles POST /api/v1/conversations/{id}/pin.
//
// Atomically sets pinned_at = now (UTC) on the
// conversation, scoped by (id, business_id, user_id) at the repository layer
// for defense-in-depth (Pitfalls §19). On success returns
// the refreshed conversation; cross-tenant attempts surface as a uniform 404
// (NEVER 403 — uniform 404 is the industry-standard guard against existence
// enumeration).
func (h *ConversationHandler) Pin(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "Pin: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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

// Unpin handles POST /api/v1/conversations/{id}/unpin.
//
// Symmetric to Pin: atomically sets
// pinned_at = nil on the conversation, scoped by (id, business_id, user_id).
// Cross-tenant attempts surface as a uniform 404.
func (h *ConversationHandler) Unpin(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "Unpin: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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
