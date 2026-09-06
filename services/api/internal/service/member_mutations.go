package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// MemberCacheInvalidator invalidates cached membership permissions.
type MemberCacheInvalidator interface {
	InvalidateMember(businessID, userID uuid.UUID)
}

// MemberMutationService applies membership mutations transactionally.
type MemberMutationService struct {
	roleRepo       domain.RoleRepository
	userRepo       domain.UserRepository
	membershipRepo domain.BusinessMembershipRepository
	invitationRepo domain.InvitationRepository
	pool           PgxBeginner
	invalidator    MemberCacheInvalidator
	audit          audit.Logger
}

// NewMemberMutationService constructs the transactional membership service.
func NewMemberMutationService(mr domain.BusinessMembershipRepository, rr domain.RoleRepository, ur domain.UserRepository, ir domain.InvitationRepository, pool PgxBeginner, inv MemberCacheInvalidator, logger audit.Logger) *MemberMutationService {
	return &MemberMutationService{rr, ur, mr, ir, pool, inv, logger}
}

// UpdateMemberRole commits membership and invitation changes before invalidating access.
func (s *MemberMutationService) UpdateMemberRole(ctx context.Context, bc authz.BusinessContext, targetUserID, roleID uuid.UUID) (*domain.BusinessMember, error) {
	rolePerms, err := s.ValidateRole(ctx, bc, roleID)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("update_member_role.begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	change := authz.OwnerChange{
		Kind:         authz.OwnerChangeDemote,
		MemberUserID: &targetUserID,
	}
	if err := authz.EnsureOwnerExistsAfter(ctx, tx, bc.BusinessID, change); err != nil {
		return nil, fmt.Errorf("update_member_role.invariant: %w", err)
	}

	if err := s.membershipRepo.UpdateRoleInTx(ctx, tx, bc.BusinessID, targetUserID, roleID, bc.UserID); err != nil {
		return nil, fmt.Errorf("update_member_role.update: %w", err)
	}

	if !slices.Contains(rolePerms, authz.PermMembersInvite) {
		if _, revokeErr := s.invitationRepo.RevokeByCreatorInTx(ctx, tx, bc.BusinessID, targetUserID); revokeErr != nil {
			return nil, fmt.Errorf("update_member_role.revoke_invitations: %w", revokeErr)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("update_member_role.commit: %w", err)
	}
	committed = true

	s.invalidator.InvalidateMember(bc.BusinessID, targetUserID)

	audit.LogRoleGranted(ctx, s.audit, bc.BusinessID, bc.UserID, targetUserID, roleID, nil)

	slog.InfoContext(ctx, "member role updated",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"target_user_id", targetUserID,
		"new_role_id", roleID,
	)

	m, err := s.membershipRepo.GetByBusinessUser(ctx, bc.BusinessID, targetUserID)
	if err != nil {
		return nil, fmt.Errorf("update_member_role.read_back: %w", err)
	}
	return m, nil
}

// RemoveMember commits membership and invitation changes before invalidating access.
func (s *MemberMutationService) RemoveMember(ctx context.Context, bc authz.BusinessContext, targetUserID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return fmt.Errorf("remove_member.begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	change := authz.OwnerChange{
		Kind:         authz.OwnerChangeRemove,
		MemberUserID: &targetUserID,
	}
	if err := authz.EnsureOwnerExistsAfter(ctx, tx, bc.BusinessID, change); err != nil {
		return fmt.Errorf("remove_member.invariant: %w", err)
	}

	if err := s.membershipRepo.DeleteInTx(ctx, tx, bc.BusinessID, targetUserID); err != nil {
		return fmt.Errorf("remove_member.delete: %w", err)
	}

	revoked, err := s.invitationRepo.RevokeByCreatorInTx(ctx, tx, bc.BusinessID, targetUserID)
	if err != nil {
		return fmt.Errorf("remove_member.revoke_invitations: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("remove_member.commit: %w", err)
	}
	committed = true

	s.invalidator.InvalidateMember(bc.BusinessID, targetUserID)

	audit.LogMemberRemoved(ctx, s.audit, bc.BusinessID, bc.UserID, targetUserID, targetUserID == bc.UserID)

	slog.InfoContext(ctx, "member removed",
		"business_id", bc.BusinessID,
		"actor_user_id", bc.UserID,
		"target_user_id", targetUserID,
		"self_removal", targetUserID == bc.UserID,
		"invitations_revoked", revoked,
	)

	return nil
}

// MemberDetails includes deletion-aware account and role information.
type MemberDetails struct {
	Member domain.BusinessMember
	User   *domain.User
	Role   *domain.Role
}

// ListMembers includes accounts throughout their deletion grace window.
func (s *MemberMutationService) ListMembers(ctx context.Context, businessID uuid.UUID) ([]MemberDetails, error) {
	members, err := s.membershipRepo.ListByBusiness(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	out := make([]MemberDetails, 0, len(members))
	for _, m := range members {
		user, err := s.userRepo.GetByIDIncludingDeleted(ctx, m.UserID)
		if err != nil {
			return nil, fmt.Errorf("get member user: %w", err)
		}
		role, err := s.roleRepo.GetByID(ctx, m.RoleID)
		if err != nil {
			return nil, fmt.Errorf("get member role: %w", err)
		}
		out = append(out, MemberDetails{m, user, role})
	}
	return out, nil
}

// ValidateRole rejects foreign roles and permission escalation.
func (s *MemberMutationService) ValidateRole(ctx context.Context, bc authz.BusinessContext, roleID uuid.UUID) ([]authz.Permission, error) {
	role, err := s.roleRepo.GetByID(ctx, roleID)
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}
	if role.BusinessID != nil && *role.BusinessID != bc.BusinessID {
		return nil, domain.ErrRoleNotFound
	}
	perms := make([]authz.Permission, 0, len(role.Permissions))
	for _, p := range role.Permissions {
		perms = append(perms, authz.Permission(p))
	}
	if err := authz.CheckEscalationSubset(bc.RoleID, bc.Permissions, perms); err != nil {
		return nil, fmt.Errorf("validate role: %w", err)
	}
	return perms, nil
}
