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
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
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

// AuditLogDTO is the historic local-name alias for the spec-side wire shape.
// Pointer fields are emitted as null when missing (no omitempty), matching
// the legacy struct semantics.
//
// Details is `map[string]interface{}` in the spec (the pkg/audit RawMessage
// is decoded into a generic map for the wire). Re-encoding into the
// response envelope is semantically equivalent but NOT byte-identical with
// the upstream Details bytes: key order is alphabetic instead of struct-
// field order, and integer floats may be re-formatted by encoding/json.
// The UI consumes details as a generic JSON object so this is acceptable;
// no downstream service (orchestrator / billing / monitoring) reads the
// audit-list output bytes.
type AuditLogDTO = openapi.AuditEvent

// AuditLogListResponse is the historic local-name alias for the spec-side
// list envelope. NextCursor is *string (omitempty in spec); the handler
// emits `"next_cursor":null` when the page is short of the limit by
// explicitly setting the field to nil (oapi-codegen drops the omitempty
// in the spec via the absence of x-go-type-extra-tags, so the field
// stays present in the JSON envelope).
type AuditLogListResponse = openapi.AuditLogListResponse

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
		slog.ErrorContext(ctx, "audit_log: list failed",
			"error", err,
			"business_id", bc.BusinessID,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	items := make([]AuditLogDTO, 0, len(rows))
	for _, l := range rows {
		items = append(items, toOpenAPIAuditEvent(l))
	}

	var next *string
	if len(rows) == limit {
		last := rows[len(rows)-1]
		c := audit.EncodeCursor(last.CreatedAt, last.ID)
		next = &c
	}

	writeJSON(w, http.StatusOK, AuditLogListResponse{Items: items, NextCursor: next})
}

// toOpenAPIAuditEvent projects a repository.AuditLogRow into the spec-side
// openapi.AuditEvent wire shape. Notes:
//
//   - Action / ActionCategory are spec-side typed strings (enum aliases);
//     the underlying JSON representation is byte-identical to the legacy
//     string fields.
//   - Pointer fields (business_id, actor_id, actor_email, actor_display_name)
//     are spec-side `required` + `nullable: true` → encoded as `null` when
//     nil. Empty actor_email / actor_display_name on the row map to nil
//     pointers so the JSON envelope emits null (matches the legacy
//     conditional pointer fix-up).
//   - Details is decoded from the pkg/audit pre-marshaled RawMessage into a
//     generic map[string]interface{} for the wire. Re-encoding into the
//     response is semantically equivalent but NOT byte-identical with the
//     upstream bytes (alphabetic key order, possible float reformatting).
//     A malformed/empty Details blob falls back to an empty object so the
//     spec `required: true` invariant is preserved.
func toOpenAPIAuditEvent(l repository.AuditLogRow) openapi.AuditEvent {
	evt := openapi.AuditEvent{
		Id:             l.ID,
		Action:         openapi.AuditAction(l.Action),
		ActionCategory: openapi.AuditEventActionCategory(audit.ActionCategory(l.Action)),
		Resource:       l.Resource,
		BusinessId:     l.BusinessID,
		ActorId:        l.UserID,
		CreatedAt:      l.CreatedAt,
		Details:        decodeAuditDetails(l.Details),
	}
	if l.ActorEmail != "" {
		email := openapi_types.Email(l.ActorEmail)
		evt.ActorEmail = &email
	}
	if l.ActorDisplayName != "" {
		name := l.ActorDisplayName
		evt.ActorDisplayName = &name
	}
	return evt
}

// decodeAuditDetails unmarshals the pkg/audit pre-marshaled Details bytes
// into a generic map. Empty / unparseable payloads fall back to an empty
// object so the spec `required: true` invariant on AuditEvent.details
// is preserved. Failure is intentionally swallowed — never block reads
// on a single bad row.
func decodeAuditDetails(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return map[string]interface{}{}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return map[string]interface{}{}
	}
	return m
}
