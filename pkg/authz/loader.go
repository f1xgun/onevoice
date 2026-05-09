package authz

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CachedMember is the membership-cache value: enough to answer "what role
// does this user have on this business and what's their status".
// Lives in pkg/authz so the loader implementation in services/api/ can
// construct values without exporting an extra adapter type.
type CachedMember struct {
	RoleID   uuid.UUID
	Status   string // "active" | "suspended"
	JoinedAt time.Time
}

// CachedRole is the role-cache value: the permissions slice the role
// grants. Phase 5 may add more fields (Description, Name) without
// breaking this contract.
type CachedRole struct {
	Permissions []Permission
}

// MembershipLoader is the DI seam between pkg/authz cache and the
// services/api repository layer. Implementations live outside pkg/authz
// (CONTEXT D-05): the production impl lives in
// services/api/internal/repository/membership_loader.go.
//
// Both methods return a sentinel domain.ErrMembershipNotFound (or
// equivalent role-not-found) on no rows. Pool-only — no transaction
// awareness (CONTEXT D-08).
type MembershipLoader interface {
	LoadMembership(ctx context.Context, businessID, userID uuid.UUID) (*CachedMember, error)
	LoadRole(ctx context.Context, roleID uuid.UUID) (*CachedRole, error)
}
