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
//
// INVITE-01: returns the raw token EXACTLY ONCE in the response body.
// INVITE-03: expires_in defaults to 7 days, range [1h, 30d].
// INVITE-04: 21st pending invitation per business → 429 too_many_pending.
// CONTEXT D-12a: cross-tenant role validation (CR-01 mirror from members.go:200).
// CONTEXT D-12b: CheckEscalationSubset at create time (system Owner exempt).
// CONTEXT D-14 + RESEARCH OQ-01: pgx.Serializable for the cap invariant.
//
// Order:
//  1. BusinessContext + Can(PermMembersInvite)
//  2. JSON decode + expires_in range validation
//  3. role lookup + CR-01 cross-tenant check
//  4. CheckEscalationSubset
//  5. GenerateInvitationToken
//  6. BeginTx(Serializable)
//  7. CountPendingByBusinessInTx; >= 20 → 429
//  8. CreateInTx
//  9. tx.Commit()
//  10. 201 + raw token in response (NO InvalidateMember — no membership changed yet)
func (h *InvitationsHandler) Create(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermMembersInvite) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req createInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if req.RoleID == uuid.Nil {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	expiresIn := req.ExpiresIn
	if expiresIn == 0 {
		expiresIn = invitationDefaultExpirySeconds
	}
	if expiresIn < invitationMinExpirySeconds || expiresIn > invitationMaxExpirySeconds {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}

	// CR-01 (CONTEXT D-12a): role must belong to this business OR be a system role.
	role, err := h.roleRepo.GetByID(r.Context(), req.RoleID)
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

	// CONTEXT D-12b: escalation-subset (system Owner exempt inside CheckEscalationSubset).
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

	// CONTEXT D-14 + RESEARCH OQ-01: Serializable so the cap holds under
	// concurrent creates. RepeatableRead in Postgres is Snapshot Isolation
	// and does NOT detect insert phantoms. See 03-RESEARCH.md §"20-Pending
	// Cap Concurrency".
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
		RoleID:     req.RoleID,
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

	// SECURITY (T-03-02): never log rawToken or hash. invitation_id is safe.
	slog.InfoContext(r.Context(), "invitation created",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"invitation_id", inv.ID,
		"role_id", inv.RoleID,
	)

	writeJSON(w, http.StatusCreated, createInvitationResponse{
		ID:        inv.ID,
		Token:     rawToken, // INVITE-01 / D-08: the ONLY response that includes the raw token
		RoleID:    inv.RoleID,
		ExpiresAt: inv.ExpiresAt.Format(time.RFC3339),
		CreatedAt: inv.CreatedAt.Format(time.RFC3339),
	})
}

// ListPending handles GET /api/v1/businesses/{id}/invitations. INVITE-05.
//
// Returns pending invitations (not accepted, not revoked, not expired)
// hydrated with role_name and inviter email per CONTEXT D-17. NEVER includes
// a raw token — the raw token is returned exactly once at create time.
func (h *InvitationsHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermMembersInvite) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	invs, err := h.invitationRepo.ListPendingByBusiness(r.Context(), bc.BusinessID)
	if err != nil {
		writeAuthzInvariantError(r.Context(), w, "list_pending_invitations", err)
		return
	}

	out := make([]listInvitationItem, 0, len(invs))
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
		out = append(out, listInvitationItem{
			ID:        inv.ID,
			RoleID:    inv.RoleID,
			RoleName:  role.Name,
			ExpiresAt: inv.ExpiresAt.UTC().Format(time.RFC3339),
			CreatedAt: inv.CreatedAt.UTC().Format(time.RFC3339),
			CreatedBy: listInvitationCreatedByPayload{ID: user.ID, Email: user.Email},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// Revoke handles DELETE /api/v1/businesses/{id}/invitations/{inviteId}.
// INVITE-06; CONTEXT D-10 (204 No Content); D-11 (404 cross-tenant, 410 terminal).
//
// Permission: PermMembersInvite. Repository scopes by (id, businessID) for
// defense-in-depth; writeRevokeError distinguishes 404 from 410.
func (h *InvitationsHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermMembersInvite) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	invID, ok := parseInvitationIDParam(w, r)
	if !ok {
		return
	}

	if err := h.invitationRepo.Revoke(r.Context(), invID, bc.BusinessID); err != nil {
		writeRevokeError(w, err)
		return
	}

	slog.InfoContext(r.Context(), "invitation revoked",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"invitation_id", invID,
	)

	w.WriteHeader(http.StatusNoContent) // CONTEXT D-10
}

// Preview handles GET /api/v1/invitations/{token} — PUBLIC. CONTEXT D-04..D-07.
//
// The token IS the auth (matches OAuth callback model). Refusal matrix
// mirrors Accept (CONTEXT D-19) — uniform 410 for unknown/expired/revoked/
// accepted to defend against token-existence enumeration. NO 409 here
// because the preview doesn't know who's calling.
//
// Information-minimization (D-06): no created_by/inviter identity, no token,
// no token_hash.
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

	// SECURITY (T-03-02): no token/hash in the slog line.
	slog.InfoContext(r.Context(), "invitation preview",
		"business_id", inv.BusinessID,
		"invitation_id", inv.ID,
	)

	writeJSON(w, http.StatusOK, previewResponse{
		BusinessID:   inv.BusinessID,
		BusinessName: biz.Name,
		RoleID:       inv.RoleID,
		RoleName:     role.Name,
		ExpiresAt:    inv.ExpiresAt.Format(time.RFC3339),
	})
}

// Accept handles POST /api/v1/invitations/{token}/accept — auth-required.
// INVITE-07..11; CONTEXT D-15 7-step ordering; RESEARCH §"Accept-Flow Concurrency".
//
// Order (D-15):
//  1. BeginTx(RepeatableRead)
//  2. invitationRepo.GetByTokenHash → 410 on miss/non-pending
//  3. membershipRepo.GetByBusinessUser → 409 already_member, NO consume (INVITE-09)
//  4. membershipRepo.Insert(tx) → PK collision → 409 already_member
//  5. invitationRepo.MarkAcceptedInTx → RowsAffected=0 → 410 with discriminator
//  6. tx.Commit()
//  7. invalidator.InvalidateMember (AFTER commit, NEVER before — INVITE-11)
func (h *InvitationsHandler) Accept(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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

	// Step 2: GetByTokenHash. Pool-based is fine — token_hash is immutable
	// post-write; the conditional UPDATE in step 5 is the race-safe primitive.
	inv, err := h.invitationRepo.GetByTokenHash(r.Context(), hash)
	if err != nil {
		writeInvitationStateError(w, err)
		return
	}
	// Pre-classify terminal states (cold-fail before touching membership).
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

	// Step 3: already-a-member? 409 + rollback (token NOT consumed — INVITE-09).
	// CONTEXT D-15 step 2 / RESEARCH OQ-05: drop the != "deleted" clause. The
	// status enum is 'active' | 'suspended'; soft-delete is by row removal.
	existing, err := h.membershipRepo.GetByBusinessUser(r.Context(), inv.BusinessID, userID)
	if err != nil && !errors.Is(err, domain.ErrMembershipNotFound) {
		writeAuthzInvariantError(r.Context(), w, "accept.membership_check", err)
		return
	}
	if existing != nil {
		// 409 already_member; tx rolls back via defer; MarkAcceptedInTx
		// is NEVER called → token NOT consumed. INVITE-09 acceptance.
		writeInvitationStateError(w, domain.ErrAlreadyMember)
		return
	}

	// Step 4: insert business_members row inside tx. PK collision → 409.
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

	// Step 5: race-safe single-use guarantee (INVITE-08).
	// On RowsAffected=0 the repo classifies the terminal state and returns
	// the right sentinel; writeInvitationStateError maps to 410 with reason.
	if err := h.invitationRepo.MarkAcceptedInTx(r.Context(), tx, inv.ID, userID); err != nil {
		writeInvitationStateError(w, err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthzInvariantError(r.Context(), w, "accept.commit", err)
		return
	}
	committed = true

	// Step 7: AFTER commit, NEVER before — INVITE-11 / CONTEXT D-15 step 7.
	h.invalidator.InvalidateMember(inv.BusinessID, userID)

	// SECURITY (T-03-02): no token/hash in the slog line.
	slog.InfoContext(r.Context(), "invitation accepted",
		"business_id", inv.BusinessID,
		"user_id", userID,
		"invitation_id", inv.ID,
	)

	writeJSON(w, http.StatusOK, acceptResponse{
		BusinessID: inv.BusinessID,
		RoleID:     inv.RoleID,
	})
}

