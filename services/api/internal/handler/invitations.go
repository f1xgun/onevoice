// Package handler — invitations.go
//
// InvitationsHandler implements INVITE-01..11:
//
//	POST   /api/v1/businesses/{id}/invitations             → Create        (PermMembersInvite + INVITE-01..04)
//	GET    /api/v1/businesses/{id}/invitations             → ListPending   (PermMembersInvite + INVITE-05)
//	DELETE /api/v1/businesses/{id}/invitations/{inviteId}  → Revoke        (PermMembersInvite + INVITE-06)
//	GET    /api/v1/invitations/{token}                      → Preview       (PUBLIC, token IS auth — D-04)
//	POST   /api/v1/invitations/{token}/accept               → Accept        (auth-required, INVITE-07..11)
//
// Mirrors members.go for poolBeginner + memberCacheInvalidator + tx-then-commit-
// then-invalidate ordering. The accept handler implements CONTEXT D-15's
// 7-step ordering with conditional-UPDATE race safety per 03-RESEARCH §"Accept-
// Flow Concurrency".
package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

const (
	invitationDefaultExpirySeconds = 7 * 24 * 3600  // 604800 — CONTEXT D-18
	invitationMinExpirySeconds     = 3600           // 1 hour
	invitationMaxExpirySeconds     = 30 * 24 * 3600 // 2592000 — 30 days
	invitationPendingCap           = 20             // INVITE-04
)

// InvitationsHandler is constructed in wire/handlers.go (plan 03-05).
// poolBeginner + memberCacheInvalidator are reused from members.go (same package).
type InvitationsHandler struct {
	invitationRepo domain.InvitationRepository
	membershipRepo domain.BusinessMembershipRepository
	roleRepo       domain.RoleRepository
	userRepo       domain.UserRepository
	businessRepo   domain.BusinessRepository
	pool           poolBeginner
	invalidator    memberCacheInvalidator
	now            func() time.Time // clock seam for test determinism
}

// NewInvitationsHandler — every dep is required.
func NewInvitationsHandler(
	ir domain.InvitationRepository,
	mr domain.BusinessMembershipRepository,
	rr domain.RoleRepository,
	ur domain.UserRepository,
	br domain.BusinessRepository,
	pool poolBeginner,
	inv memberCacheInvalidator,
) (*InvitationsHandler, error) {
	if ir == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: invitationRepo cannot be nil")
	}
	if mr == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: membershipRepo cannot be nil")
	}
	if rr == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: roleRepo cannot be nil")
	}
	if ur == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: userRepo cannot be nil")
	}
	if br == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: businessRepo cannot be nil")
	}
	if pool == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: pool cannot be nil")
	}
	if inv == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: invalidator cannot be nil")
	}
	return &InvitationsHandler{
		invitationRepo: ir,
		membershipRepo: mr,
		roleRepo:       rr,
		userRepo:       ur,
		businessRepo:   br,
		pool:           pool,
		invalidator:    inv,
		now:            time.Now,
	}, nil
}

// --- Request / response types ---

type createInvitationRequest struct {
	RoleID    uuid.UUID `json:"role_id"`
	ExpiresIn int       `json:"expires_in,omitempty"` // seconds; CONTEXT D-18; range [3600, 2592000]
}

type createInvitationResponse struct {
	ID        uuid.UUID `json:"id"`
	Token     string    `json:"token"` // raw token — returned ONCE per INVITE-01 / D-08
	RoleID    uuid.UUID `json:"role_id"`
	ExpiresAt string    `json:"expires_at"` // RFC3339
	CreatedAt string    `json:"created_at"`
}

type listInvitationItem struct {
	ID        uuid.UUID                      `json:"id"`
	RoleID    uuid.UUID                      `json:"role_id"`
	RoleName  string                         `json:"role_name"`
	ExpiresAt string                         `json:"expires_at"`
	CreatedAt string                         `json:"created_at"`
	CreatedBy listInvitationCreatedByPayload `json:"created_by"`
}

type listInvitationCreatedByPayload struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
}

type previewResponse struct {
	BusinessID   uuid.UUID `json:"business_id"`
	BusinessName string    `json:"business_name"`
	RoleID       uuid.UUID `json:"role_id"`
	RoleName     string    `json:"role_name"`
	ExpiresAt    string    `json:"expires_at"`
}

type acceptResponse struct {
	BusinessID uuid.UUID `json:"business_id"`
	RoleID     uuid.UUID `json:"role_id"`
}

// --- Helpers ---

// parseInvitationIDParam extracts {inviteId} from chi URL params.
// Mirrors parseMemberUserIDParam in members.go:358.
func parseInvitationIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := chi.URLParam(r, "inviteId")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_invite_id")
		return uuid.Nil, false
	}
	return id, true
}

// computeTokenHash hashes the URL-supplied raw token using the same
// hex(sha256(raw)) representation as repository.GenerateInvitationToken.
// SECURITY:
//   - Hash equality on the UNIQUE token_hash B-tree index is the timing-safe
//     primitive (research §"Token Hashing & Lookup"). The repository's
//     GetByTokenHash adds an explicit subtle.ConstantTimeCompare on the
//     already-retrieved hash as a no-op defense-in-depth check and to
//     satisfy the literal INVITE-02 contract phrase.
//   - Never log the raw token or the hash. Only invitation_id is safe to log.
func computeTokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// --- Handler stubs (filled in Tasks 2 + 3 of this plan) ---

// Create handles POST /api/v1/businesses/{id}/invitations.
func (h *InvitationsHandler) Create(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusNotImplemented, "not_implemented")
}

// ListPending handles GET /api/v1/businesses/{id}/invitations.
func (h *InvitationsHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusNotImplemented, "not_implemented")
}

// Revoke handles DELETE /api/v1/businesses/{id}/invitations/{inviteId}.
func (h *InvitationsHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusNotImplemented, "not_implemented")
}

// Preview handles GET /api/v1/invitations/{token} — PUBLIC.
func (h *InvitationsHandler) Preview(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusNotImplemented, "not_implemented")
}

// Accept handles POST /api/v1/invitations/{token}/accept — auth-required.
func (h *InvitationsHandler) Accept(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusNotImplemented, "not_implemented")
}

// --- Compile-time guards: tasks 2/3 will use these symbols ---
var (
	_ = repository.GenerateInvitationToken
	_ = json.NewDecoder
	_ = errors.Is
	_ = slog.Default
	_ = context.Background
	_ = pgx.Serializable
	_ = middleware.GetUserID
	_ = authz.PermMembersInvite
)
