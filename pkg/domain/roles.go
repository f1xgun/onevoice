package domain

// UserRole is the legacy single-owner-per-business enum stored on
// users.role. Renamed from Role to UserRole in Phase 1 of the v2.0 RBAC
// milestone so the canonical Role identifier is free for the new
// pkg/domain.Role aggregate (mirrors the roles table). The legacy enum
// itself is left populated and ignored at runtime per CONTEXT decision
// D-13 / SPEC out-of-scope CLEAN-03 — Phase 6 deletes this file along
// with the users.role column.
//
// Old name → new name mapping for grep:
//
//	Role        → UserRole
//	RoleOwner   → UserRoleOwner
//	RoleAdmin   → UserRoleAdmin
//	RoleMember  → UserRoleMember
type UserRole string

const (
	UserRoleOwner  UserRole = "owner"
	UserRoleAdmin  UserRole = "admin"
	UserRoleMember UserRole = "member"
)

func (r UserRole) IsValid() bool {
	return r == UserRoleOwner || r == UserRoleAdmin || r == UserRoleMember
}

func (r UserRole) String() string {
	return string(r)
}
