// Package handler — audit_log.go
//
// AuditLogHandler serves the Phase 19 read path:
//   GET /api/v1/businesses/{id}/audit-logs
//
// Wave 5 (Plan 19-05). Backed by repository.AuditLogRepository's new
// ListByBusinessWithActors method (Wave 5 too) which does the LEFT JOIN
// users in one query — the handler MUST NOT fan out per-row user lookups
// into UserRepository, that would be the N+1 anti-pattern the join was
// added to prevent (per 19-RESEARCH §"DTO Enrichment Strategy" + CHECK F-7).
//
// Cursor pagination uses pkg/audit.EncodeCursor / DecodeCursor so the
// opaque token round-trips between the handler and the next request
// transparently. RBAC guard: PermAuditRead (Owner+Admin via Phase 6
// seed). RequireBusinessAccess middleware on the parent /businesses/{id}
// subtree already enforces membership; the handler-level Can() check
// gates the more specific permission per the audit category.
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

// AuditLogLister is the narrow read surface the handler depends on. The
// concrete *repository.AuditLogRepository satisfies this — the handler
// imports the repository package directly because ListByBusinessWithActors
// returns a repository-package type (AuditLogRow) that's intentionally NOT
// part of domain.AuditLogRepository. Keeping the interface narrow lets the
// handler test stub a single method without dragging in the full repo
// surface (Insert / DeleteOlderThan / ListByBusiness).
//
// Exported so the wire layer (services/api/internal/wire/handlers.go) can
// type-assert the domain.AuditLogRepository the Repos struct holds into
// this narrow shape without each consumer rolling its own interface.
type AuditLogLister interface {
	ListByBusinessWithActors(ctx context.Context, businessID uuid.UUID, f domain.AuditLogFilter) ([]repository.AuditLogRow, error)
}

// AuditLogHandler implements GET /businesses/{id}/audit-logs.
//
// The handler is deliberately stateless beyond the repo reference — no
// per-request caches, no in-memory rate limits (handled by the global
// middleware), no body parsing (GET only).
type AuditLogHandler struct {
	repo AuditLogLister
}

// NewAuditLogHandler constructs an AuditLogHandler. repo must satisfy
// AuditLogLister — production wires the concrete repository.AuditLog
// constructor here, tests inject a stub.
func NewAuditLogHandler(repo AuditLogLister) *AuditLogHandler {
	return &AuditLogHandler{repo: repo}
}

// AuditLogDTO is the wire shape for a single audit_logs row. Pointer
// fields use the omitempty-friendly *uuid.UUID / *string idiom so the JSON
// encoder emits `null` for missing actors / business / display names —
// the frontend zod schema accepts both null and absent.
//
// Details is passed through as json.RawMessage so each builder's typed
// Details struct (pkg/audit/details.go) round-trips byte-for-byte without
// the handler re-parsing the JSONB column.
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
// NextCursor is non-nil ONLY when the returned page is full AND there
// might be another page — the handler sets it from the last row's
// (created_at, id) tuple. A null NextCursor signals "end of stream"
// to the frontend's infinite-scroll loader.
type AuditLogListResponse struct {
	Items      []AuditLogDTO `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}

// knownActions is the closed validation set for the ?action= query
// parameter. Cardinality is bounded by pkg/audit/actions.go — adding a new
// audit action requires (a) a const there + builder, (b) wiring at the
// call site, AND (c) an entry here. The drift surface is small (21 today)
// and the failure mode is explicit (400 invalid_action) which catches
// frontend typos at the boundary.
var knownActions = map[string]struct{}{
	audit.ActionRoleGranted:               {},
	audit.ActionMemberRemoved:             {},
	audit.ActionRoleCreated:               {},
	audit.ActionRoleUpdated:               {},
	audit.ActionRoleDeleted:               {},
	audit.ActionInvitationCreated:         {},
	audit.ActionInvitationRevoked:         {},
	audit.ActionInvitationAccepted:        {},
	audit.ActionLoginSuccess:              {},
	audit.ActionLoginFailed:               {},
	audit.ActionLogout:                    {},
	audit.ActionPasswordChanged:           {},
	audit.ActionUserRegistered:            {},
	audit.ActionIntegrationConnected:      {},
	audit.ActionIntegrationDisconnected:   {},
	audit.ActionIntegrationTokenRotated:   {},
	audit.ActionBusinessCreated:           {},
	audit.ActionBusinessUpdated:           {},
	audit.ActionProjectCreated:            {},
	audit.ActionProjectUpdated:            {},
	audit.ActionProjectDeleted:            {},
}

// knownCategories is the closed set the frontend tab filter uses. Matches
// the prefix list audit.ActionCategory() returns; "other" is intentionally
// excluded because no live action emits an unknown prefix today and
// accepting "other" would let a caller probe for prefix-drift.
var knownCategories = map[string]struct{}{
	"rbac":        {},
	"auth":        {},
	"integration": {},
	"business":    {},
	"project":     {},
}

// Limit bounds for the ?limit= query param. Defense-in-depth: the
// repository also clamps (defaultListLimit / maxListLimit constants in
// audit_log.go) so a malformed handler cannot trigger an unbounded scan,
// but the handler returns 400 invalid_limit before hitting the repo to
// give the frontend a fast feedback loop on bad input.
const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// List handles GET /api/v1/businesses/{id}/audit-logs.
//
// Order of operations:
//   1. BusinessContext from ctx (500 if missing — programmer error: route
//      registered outside /businesses/{id} subtree)
//   2. authz.Can(PermAuditRead) (403 forbidden if absent)
//   3. Parse + validate query params (400 with code-specific error body)
//   4. Repo call (single LEFT JOIN, no N+1)
//   5. DTO build (ActorEmail "" → nil pointer so JSON emits null)
//   6. Cursor build (only when page is full)
//   7. 200 with {items, next_cursor}
//
// On repo error: 500 internal_server_error + slog.Error with biz_id +
// the raw error. NEVER leak repo error details to the client (it could
// disclose SQL shape / schema metadata).
func (h *AuditLogHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bc, ok := authz.BusinessContextFromCtx(ctx)
	if !ok {
		// Programmer error: this route MUST live inside the
		// /businesses/{id} subtree. lint-rbac would catch a future
		// regression at PR time; the runtime 500 here is the
		// belt-and-braces backstop.
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
			// audit.ErrInvalidCursor wraps all "bad input" causes
			// (empty / non-base64 / non-JSON / missing fields / bad
			// UUID / bad RFC3339). Map to a single 400 invalid_cursor.
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
		// Repo errors are wrapped pgx errors / connection drops; NEVER
		// surface them to the client. Log with biz_id + correlation_id
		// (slog middleware attaches the latter) and return 500.
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
		// "" → nil-pointer mapping. Empty actor_email means either
		// (a) audit_logs.user_id was NULL (failed-login per D-31) or
		// (b) the LEFT JOIN found no matching users row (deleted user).
		// Either way the JSON contract surfaces `actor_email: null`
		// which the frontend handles by rendering
		// "Неизвестен ({attempted_email})" via details.attempted_email.
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

	// Cursor build: only when the page is full AND the cursor is
	// derived from the LAST row's (created_at, id). If the page came
	// back short the caller has reached the end of the stream — emit
	// `next_cursor: null` so the frontend's "Load more" button hides
	// itself. Subtle: a page that is exactly limit-sized AND happens to
	// be the last page will return a non-nil cursor — the next request
	// will get an empty page and stop. That's one wasted round-trip per
	// stream-end and the cost of not over-fetching (Wave 3 picked the
	// simpler semantic over a +1 row sentinel).
	var next *string
	if len(rows) == limit {
		last := rows[len(rows)-1]
		c := audit.EncodeCursor(last.CreatedAt, last.ID)
		next = &c
	}

	writeJSON(w, http.StatusOK, AuditLogListResponse{Items: items, NextCursor: next})
}
