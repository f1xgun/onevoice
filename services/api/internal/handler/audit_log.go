// Package handler — audit_log.go.
//
// See docs/api/handlers/audit-log.md for the filter shape, cursor pagination
// semantics, and ACL boundary discussion.
package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// AuditLogLister is the narrow read surface the handler depends on.
// Exported so wire/ can type-assert domain.AuditLogRepository into this shape.
type AuditLogLister interface {
	ListByBusinessWithActors(ctx context.Context, businessID uuid.UUID, f domain.AuditLogFilter) ([]repository.AuditLogRow, error)
}

// AuditLogHandler implements GET /businesses/{id}/audit-logs.
type AuditLogHandler struct {
	repo AuditLogLister
}

// NewAuditLogHandler constructs an AuditLogHandler.
func NewAuditLogHandler(repo AuditLogLister) *AuditLogHandler {
	return &AuditLogHandler{repo: repo}
}

// AuditLogDTO is the wire shape for a single audit_logs row.
// Pointer fields use omitempty so the encoder emits null for missing actors.
// Details is json.RawMessage to round-trip pkg/audit Details structs byte-for-byte.
type AuditLogDTO struct {
	ID               uuid.UUID       `json:"id"`
	Action           string          `json:"action"`
	ActionCategory   string          `json:"action_category"`
	Resource         string          `json:"resource"`
	BusinessID       *uuid.UUID      `json:"business_id"`
	ActorID          *uuid.UUID      `json:"actor_id"`
	ActorEmail       *string         `json:"actor_email"`
	ActorDisplayName *string         `json:"actor_display_name"`
	Details          json.RawMessage `json:"details"`
	CreatedAt        time.Time       `json:"created_at"`
}

// AuditLogListResponse is the wire shape for the list endpoint.
// NextCursor is non-nil ONLY when the returned page is full; null = end of stream.
type AuditLogListResponse struct {
	Items      []AuditLogDTO `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}

// knownActions is the closed validation set for the ?action= query parameter.
// Adding a new audit action requires a matching entry here (failure mode: 400 invalid_action).
var knownActions = map[string]struct{}{
	audit.ActionRoleGranted:             {},
	audit.ActionMemberRemoved:           {},
	audit.ActionRoleCreated:             {},
	audit.ActionRoleUpdated:             {},
	audit.ActionRoleDeleted:             {},
	audit.ActionInvitationCreated:       {},
	audit.ActionInvitationRevoked:       {},
	audit.ActionInvitationAccepted:      {},
	audit.ActionLoginSuccess:            {},
	audit.ActionLoginFailed:             {},
	audit.ActionLogout:                  {},
	audit.ActionPasswordChanged:         {},
	audit.ActionUserRegistered:          {},
	audit.ActionIntegrationConnected:    {},
	audit.ActionIntegrationDisconnected: {},
	audit.ActionIntegrationTokenRotated: {},
	audit.ActionBusinessCreated:         {},
	audit.ActionBusinessUpdated:         {},
	audit.ActionProjectCreated:          {},
	audit.ActionProjectUpdated:          {},
	audit.ActionProjectDeleted:          {},
}

// knownCategories is the closed set the frontend tab filter uses.
// "other" is intentionally excluded — accepting it would let a caller probe for prefix-drift.
var knownCategories = map[string]struct{}{
	"rbac":        {},
	"auth":        {},
	"integration": {},
	"business":    {},
	"project":     {},
}

// Limit bounds for the ?limit= query param; the repo also clamps as defense-in-depth.
const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// List handles GET /api/v1/businesses/{id}/audit-logs.
// See docs/api/handlers/audit-log.md.
func (h *AuditLogHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bc, ok := authz.BusinessContextFromCtx(ctx)
	if !ok {
		// Programmer error: route must live inside /businesses/{id} subtree.
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(ctx, authz.PermAuditRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	q := r.URL.Query()
	filter := domain.AuditLogFilter{}

	if c := q.Get("category"); c != "" {
		if _, ok := knownCategories[c]; !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid_category")
			return
		}
		filter.Category = c
	}
	if a := q.Get("action"); a != "" {
		if _, ok := knownActions[a]; !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid_action")
			return
		}
		filter.Action = a
	}
	if actor := q.Get("actor"); actor != "" {
		id, err := uuid.Parse(actor)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_actor")
			return
		}
		filter.ActorID = &id
	}
	if fromStr := q.Get("from"); fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_date")
			return
		}
		filter.From = &t
	}
	if toStr := q.Get("to"); toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_date")
			return
		}
		filter.To = &t
	}
	if cursor := q.Get("cursor"); cursor != "" {
		t, id, err := audit.DecodeCursor(cursor)
		if err != nil {
			// All decode causes collapse to one 400 — distinguishing them
			// client-side would only help an attacker probe the cursor format.
			writeJSONError(w, http.StatusBadRequest, "invalid_cursor")
			return
		}
		filter.CursorTime = &t
		filter.CursorID = &id
	}

	limit := defaultPageLimit
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > maxPageLimit {
			writeJSONError(w, http.StatusBadRequest, "invalid_limit")
			return
		}
		limit = n
	}
	filter.Limit = limit

	rows, err := h.repo.ListByBusinessWithActors(ctx, bc.BusinessID, filter)
	if err != nil {
		// NEVER surface repo errors — they could disclose SQL shape / schema.
		slog.ErrorContext(ctx, "audit_log: list failed",
			"error", err,
			"business_id", bc.BusinessID,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	items := make([]AuditLogDTO, 0, len(rows))
	for _, l := range rows {
		dto := AuditLogDTO{
			ID:             l.ID,
			Action:         l.Action,
			ActionCategory: audit.ActionCategory(l.Action),
			Resource:       l.Resource,
			BusinessID:     l.BusinessID,
			ActorID:        l.UserID,
			Details:        l.Details,
			CreatedAt:      l.CreatedAt,
		}
		// "" → nil-pointer so JSON emits null. Covers failed-login (user_id NULL)
		// and deleted-user (LEFT JOIN miss); frontend renders both as unknown.
		if l.ActorEmail != "" {
			email := l.ActorEmail
			dto.ActorEmail = &email
		}
		if l.ActorDisplayName != "" {
			name := l.ActorDisplayName
			dto.ActorDisplayName = &name
		}
		items = append(items, dto)
	}

	// Cursor only when the page is full; short page → next_cursor: null = end of stream.
	var next *string
	if len(rows) == limit {
		last := rows[len(rows)-1]
		c := audit.EncodeCursor(last.CreatedAt, last.ID)
		next = &c
	}

	writeJSON(w, http.StatusOK, AuditLogListResponse{Items: items, NextCursor: next})
}
