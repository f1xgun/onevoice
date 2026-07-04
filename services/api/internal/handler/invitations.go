// Package handler — invitations.go.
//
// See docs/api/handlers/invitations.md for the invitation lifecycle, ACL
// boundaries, and the race-safe single-use guarantee on accept.
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
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

const (
	invitationDefaultExpirySeconds = 7 * 24 * 3600  // 7 days
	invitationMinExpirySeconds     = 3600           // 1 hour
	invitationMaxExpirySeconds     = 30 * 24 * 3600 // 30 days
	invitationPendingCap           = 20
)

// InvitationsHandler serves the invitation CRUD + accept endpoints.
type InvitationsHandler struct {
	invitationRepo  domain.InvitationRepository
	membershipRepo  domain.BusinessMembershipRepository
	roleRepo        domain.RoleRepository
	userRepo        domain.UserRepository
	businessRepo    domain.BusinessRepository
	businessService businessGetter
	pool            poolBeginner
	invalidator     memberCacheInvalidator
	audit           audit.Logger
	now             func() time.Time // clock seam for test determinism
}

// NewInvitationsHandler constructs an InvitationsHandler; every dep is required.
// businessService lets the write endpoints reject mutations against a
// soft-deleted (erasure-pending) organization that RequireBusinessAccess
// (membership-only) does not gate.
func NewInvitationsHandler(
	ir domain.InvitationRepository,
	mr domain.BusinessMembershipRepository,
	rr domain.RoleRepository,
	ur domain.UserRepository,
	br domain.BusinessRepository,
	bs businessGetter,
	pool poolBeginner,
	inv memberCacheInvalidator,
	auditLogger audit.Logger,
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
	if bs == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: businessService cannot be nil")
	}
	if pool == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: pool cannot be nil")
	}
	if inv == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: invalidator cannot be nil")
	}
	if auditLogger == nil {
		return nil, fmt.Errorf("NewInvitationsHandler: auditLogger cannot be nil")
	}
	return &InvitationsHandler{
		invitationRepo:  ir,
		membershipRepo:  mr,
		roleRepo:        rr,
		userRepo:        ur,
		businessRepo:    br,
		businessService: bs,
		pool:            pool,
		invalidator:     inv,
		audit:           auditLogger,
		now:             time.Now,
	}, nil
}

// --- Domain → openapi mappers ---

// domainInvitationToCreateResponse maps a freshly-inserted Invitation + its
// raw token (returned ONCE on create only) into the spec-side
// CreateInvitationResponse wire shape. Time fields are UTC-normalized so the
// JSON serializer (time.Time → RFC3339Nano) emits a stable representation.
func domainInvitationToCreateResponse(inv *domain.Invitation, rawToken string) openapi.CreateInvitationResponse {
	return openapi.CreateInvitationResponse{
		Id:        inv.ID,
		Token:     rawToken,
		RoleId:    inv.RoleID,
		ExpiresAt: inv.ExpiresAt.UTC(),
		CreatedAt: inv.CreatedAt.UTC(),
	}
}

// domainInvitationToPending maps a pending Invitation + its hydrated role/user
// into the spec-side PendingInvitation wire shape.
func domainInvitationToPending(inv *domain.Invitation, roleName string, createdBy *domain.User) openapi.PendingInvitation {
	out := openapi.PendingInvitation{
		Id:        inv.ID,
		RoleId:    inv.RoleID,
		RoleName:  roleName,
		ExpiresAt: inv.ExpiresAt.UTC(),
		CreatedAt: inv.CreatedAt.UTC(),
	}
	out.CreatedBy.Id = createdBy.ID
	out.CreatedBy.Email = openapi_types.Email(createdBy.Email)
	return out
}

// domainInvitationToPreview maps an Invitation + its hydrated business/role
// names into the spec-side InvitationPreview wire shape used by the public
// preview endpoint.
func domainInvitationToPreview(inv *domain.Invitation, businessName, roleName string) openapi.InvitationPreview {
	return openapi.InvitationPreview{
		BusinessId:   inv.BusinessID,
		BusinessName: businessName,
		RoleId:       inv.RoleID,
		RoleName:     roleName,
		ExpiresAt:    inv.ExpiresAt.UTC(),
	}
}

// parseInvitationIDParam extracts {inviteId} from chi URL params; writes 400 on failure.
func parseInvitationIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	return parseUUIDParam(w, r, "inviteId", "invalid_invite_id")
}

// computeTokenHash hashes the raw token with hex(sha256) to match the
// repository's stored representation.
// SECURITY: never log the raw token or the hash; only invitation_id is safe.
func computeTokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Create handles POST /api/v1/businesses/{id}/invitations.
// See docs/api/handlers/invitations.md.
func (h *InvitationsHandler) Create(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "", authz.PermMembersInvite)
	if !ok {
		return
	}

	if _, err := h.businessService.GetByID(r.Context(), bc.BusinessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "create invitation: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var req openapi.CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if req.RoleId == uuid.Nil {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	expiresIn := invitationDefaultExpirySeconds
	if req.ExpiresIn != nil {
		expiresIn = *req.ExpiresIn
	}
	if expiresIn < invitationMinExpirySeconds || expiresIn > invitationMaxExpirySeconds {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}

	role, err := h.roleRepo.GetByID(r.Context(), req.RoleId)
	if err != nil {
		if errors.Is(err, domain.ErrRoleNotFound) {
			writeJSONError(w, http.StatusBadRequest, "invalid_role_id")
			return
		}
		writeAuthzInvariantError(r.Context(), w, "create_invitation.role_lookup", err)
		return
	}
	if role.BusinessID != nil && *role.BusinessID != bc.BusinessID {
		writeJSONError(w, http.StatusBadRequest, "invalid_role_id")
		return
	}

	rolePerms := make([]authz.Permission, 0, len(role.Permissions))
	for _, p := range role.Permissions {
		rolePerms = append(rolePerms, authz.Permission(p))
	}
	if err := authz.CheckEscalationSubset(bc.RoleID, bc.Permissions, rolePerms); err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_invitation.escalation", err)
		return
	}

	rawToken, hash, err := repository.GenerateInvitationToken()
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_invitation.token", err)
		return
	}

	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_invitation.begin", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	count, err := h.invitationRepo.CountPendingByBusinessInTx(r.Context(), tx, bc.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_invitation.count", err)
		return
	}
	if count >= invitationPendingCap {
		writeJSONError(w, http.StatusTooManyRequests, "too_many_pending")
		return
	}

	now := h.now().UTC()
	inv := &domain.Invitation{
		BusinessID: bc.BusinessID,
		RoleID:     req.RoleId,
		TokenHash:  hash,
		ExpiresAt:  now.Add(time.Duration(expiresIn) * time.Second),
		CreatedBy:  bc.UserID,
		CreatedAt:  now,
	}
	if err := h.invitationRepo.CreateInTx(r.Context(), tx, inv); err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_invitation.insert", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "create_invitation.commit", err)
		return
	}
	committed = true

	slog.InfoContext(r.Context(), "invitation created",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"invitation_id", inv.ID,
		"role_id", inv.RoleID,
	)

	audit.LogInvitationCreated(r.Context(), h.audit, bc.BusinessID, bc.UserID, inv.ID, inv.RoleID, inv.ExpiresAt)

	writeJSON(w, http.StatusCreated, domainInvitationToCreateResponse(inv, rawToken))
}

// ListPending handles GET /api/v1/businesses/{id}/invitations.
// See docs/api/handlers/invitations.md.
func (h *InvitationsHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "", authz.PermMembersInvite)
	if !ok {
		return
	}

	invs, err := h.invitationRepo.ListPendingByBusiness(r.Context(), bc.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "list_pending_invitations", err)
		return
	}

	out := make([]openapi.PendingInvitation, 0, len(invs))
	for _, inv := range invs {
		role, err := h.roleRepo.GetByID(r.Context(), inv.RoleID)
		if err != nil {
			writeAuthzInvariantError(r.Context(), w, "list_pending.role_lookup", err)
			return
		}
		user, err := h.userRepo.GetByID(r.Context(), inv.CreatedBy)
		if err != nil {
			writeAuthzInvariantError(r.Context(), w, "list_pending.user_lookup", err)
			return
		}
		invCopy := inv
		out = append(out, domainInvitationToPending(&invCopy, role.Name, user))
	}
	writeJSON(w, http.StatusOK, out)
}

// Revoke handles DELETE /api/v1/businesses/{id}/invitations/{inviteId}.
// See docs/api/handlers/invitations.md.
func (h *InvitationsHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "", authz.PermMembersInvite)
	if !ok {
		return
	}
	invID, ok := parseInvitationIDParam(w, r)
	if !ok {
		return
	}

	if _, err := h.businessService.GetByID(r.Context(), bc.BusinessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "revoke invitation: failed to resolve business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.invitationRepo.Revoke(r.Context(), invID, bc.BusinessID); err != nil {
		writeRevokeError(w, err)
		return
	}

	audit.LogInvitationRevoked(r.Context(), h.audit, bc.BusinessID, bc.UserID, invID)

	slog.InfoContext(r.Context(), "invitation revoked",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"invitation_id", invID,
	)

	w.WriteHeader(http.StatusNoContent)
}

// Preview handles GET /api/v1/invitations/{token} — PUBLIC; the token IS the auth.
// See docs/api/handlers/invitations.md.
func (h *InvitationsHandler) Preview(w http.ResponseWriter, r *http.Request) {
	rawToken := chi.URLParam(r, "token")
	if rawToken == "" {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	hash := computeTokenHash(rawToken)

	inv, err := h.invitationRepo.GetByTokenHash(r.Context(), hash)
	if err != nil {
		writeInvitationStateError(w, err)
		return
	}
	if inv.AcceptedAt != nil {
		writeInvitationStateError(w, domain.ErrInvitationAccepted)
		return
	}
	if inv.RevokedAt != nil {
		writeInvitationStateError(w, domain.ErrInvitationRevoked)
		return
	}
	if !inv.ExpiresAt.After(h.now()) {
		writeInvitationStateError(w, domain.ErrInvitationExpired)
		return
	}

	role, err := h.roleRepo.GetByID(r.Context(), inv.RoleID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "preview.role_lookup", err)
		return
	}
	biz, err := h.businessRepo.GetByID(r.Context(), inv.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "preview.business_lookup", err)
		return
	}

	slog.InfoContext(r.Context(), "invitation preview",
		"business_id", inv.BusinessID,
		"invitation_id", inv.ID,
	)

	writeJSON(w, http.StatusOK, domainInvitationToPreview(inv, biz.Name, role.Name))
}

// Accept handles POST /api/v1/invitations/{token}/accept — auth-required, NOT business-scoped.
// See docs/api/handlers/invitations.md for the race-safe accept order.
func (h *InvitationsHandler) Accept(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	rawToken := chi.URLParam(r, "token")
	if rawToken == "" {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	hash := computeTokenHash(rawToken)

	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "accept.begin", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	inv, err := h.invitationRepo.GetByTokenHash(r.Context(), hash)
	if err != nil {
		writeInvitationStateError(w, err)
		return
	}
	if inv.AcceptedAt != nil {
		writeInvitationStateError(w, domain.ErrInvitationAccepted)
		return
	}
	if inv.RevokedAt != nil {
		writeInvitationStateError(w, domain.ErrInvitationRevoked)
		return
	}
	if !inv.ExpiresAt.After(h.now()) {
		writeInvitationStateError(w, domain.ErrInvitationExpired)
		return
	}

	existing, err := h.membershipRepo.GetByBusinessUser(r.Context(), inv.BusinessID, userID)
	if err != nil && !errors.Is(err, domain.ErrMembershipNotFound) {
		writeAuthzInvariantError(r.Context(), w, "accept.membership_check", err)
		return
	}
	if existing != nil {
		writeInvitationStateError(w, domain.ErrAlreadyMember)
		return
	}

	now := h.now().UTC()
	createdAt := inv.CreatedAt
	createdBy := inv.CreatedBy
	member := &domain.BusinessMember{
		BusinessID: inv.BusinessID,
		UserID:     userID,
		RoleID:     inv.RoleID,
		Status:     "active",
		InvitedBy:  &createdBy,
		InvitedAt:  &createdAt,
		JoinedAt:   now,
	}
	if err := h.membershipRepo.Insert(r.Context(), tx, member); err != nil {
		if errors.Is(err, domain.ErrMembershipExists) {
			writeInvitationStateError(w, domain.ErrAlreadyMember)
			return
		}
		writeAuthzInvariantError(r.Context(), w, "accept.insert_member", err)
		return
	}

	if err := h.invitationRepo.MarkAcceptedInTx(r.Context(), tx, inv.ID, userID); err != nil {
		writeInvitationStateError(w, err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "accept.commit", err)
		return
	}
	committed = true

	h.invalidator.InvalidateMember(inv.BusinessID, userID)

	audit.LogInvitationAccepted(r.Context(), h.audit, inv.BusinessID, userID, inv.ID, inv.RoleID)

	slog.InfoContext(r.Context(), "invitation accepted",
		"business_id", inv.BusinessID,
		"user_id", userID,
		"invitation_id", inv.ID,
	)

	writeJSON(w, http.StatusOK, openapi.AcceptInvitationResponse{
		BusinessId: inv.BusinessID,
		RoleId:     inv.RoleID,
	})
}
