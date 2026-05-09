// Package authz — permissions.go
//
// The typed permission registry. Phase 1 ships the constants and the
// AllPermissions() accessor; Phase 2 adds cache.go, check.go, loader.go to
// this same package (CONTEXT D-10/D-11). All permissions are flat
// resource.action strings matching ^[a-z_]+\.[a-z_]+$ — no wildcards, no
// hierarchy (REQUIREMENTS Out-of-Scope §"Hierarchical / wildcard").
//
// CHANGES TO THIS FILE MUST BE MIRRORED IN THE MIGRATION SEED.
// migrations/postgres/000006_rbac_data_model.up.sql and
// services/api/migrations/000005_rbac_data_model.up.sql each carry a
// hardcoded JSONB array per system role; drift is caught by
// test/integration/system_roles_test.go (Plan H) which queries the
// seeded JSONB and asserts equality with the registry.
package authz

// Permission is a flat resource.action string. The named type lets handlers
// pass typed values to authz.Can() (Phase 2) and gives the JSON encoder a
// stable wire shape via the underlying string.
type Permission string

// PermissionMeta is the registry entry for one permission. Description is
// empty in Phase 1; Phase 5 fills it for the role-editor tooltip UX.
type PermissionMeta struct {
	Name        Permission `json:"name"`
	Description string     `json:"description"`
}

// PermissionGroup is the response shape returned by GET /api/v1/permissions
// (Plan G). Resource is the lowercase resource segment ("business",
// "members", etc.); Permissions are ordered by verb in registry order
// (read, then mutating verbs).
type PermissionGroup struct {
	Resource    string           `json:"resource"`
	Permissions []PermissionMeta `json:"permissions"`
}

// Permission constants — exported names follow `Perm<Resource><Action>`
// PascalCase. Adding a new permission means: add the const here, add it to
// the appropriate group in AllPermissions, mirror it in the migration seed
// (and re-run the Phase 1 drift test), and grant it to the relevant system
// roles in the seed JSONB arrays.
const (
	// business.*
	PermBusinessRead              Permission = "business.read"
	PermBusinessUpdate            Permission = "business.update"
	PermBusinessDelete            Permission = "business.delete"
	PermBusinessTransferOwnership Permission = "business.transfer_ownership"

	// members.*
	PermMembersRead       Permission = "members.read"
	PermMembersInvite     Permission = "members.invite"
	PermMembersRemove     Permission = "members.remove"
	PermMembersUpdateRole Permission = "members.update_role"

	// roles.*
	PermRolesRead   Permission = "roles.read"
	PermRolesCreate Permission = "roles.create"
	PermRolesUpdate Permission = "roles.update"
	PermRolesDelete Permission = "roles.delete"

	// integrations.*
	PermIntegrationsRead       Permission = "integrations.read"
	PermIntegrationsConnect    Permission = "integrations.connect"
	PermIntegrationsDisconnect Permission = "integrations.disconnect"

	// content.*
	PermContentRead   Permission = "content.read"
	PermContentCreate Permission = "content.create"
	PermContentUpdate Permission = "content.update"
	PermContentDelete Permission = "content.delete"

	// billing.*
	PermBillingRead   Permission = "billing.read"
	PermBillingUpdate Permission = "billing.update"
)

// AllPermissions returns the registry grouped by resource, in the order
// {business, members, roles, integrations, content, billing}. Each group's
// permissions are ordered by registry-declaration order (read first, then
// mutating verbs). The handler in Plan G serializes this directly as JSON.
//
// The function returns a fresh slice on every call so callers cannot mutate
// shared state. Cost is negligible: 6 groups × small slices.
func AllPermissions() []PermissionGroup {
	return []PermissionGroup{
		{Resource: "business", Permissions: []PermissionMeta{
			{Name: PermBusinessRead, Description: ""},
			{Name: PermBusinessUpdate, Description: ""},
			{Name: PermBusinessDelete, Description: ""},
			{Name: PermBusinessTransferOwnership, Description: ""},
		}},
		{Resource: "members", Permissions: []PermissionMeta{
			{Name: PermMembersRead, Description: ""},
			{Name: PermMembersInvite, Description: ""},
			{Name: PermMembersRemove, Description: ""},
			{Name: PermMembersUpdateRole, Description: ""},
		}},
		{Resource: "roles", Permissions: []PermissionMeta{
			{Name: PermRolesRead, Description: ""},
			{Name: PermRolesCreate, Description: ""},
			{Name: PermRolesUpdate, Description: ""},
			{Name: PermRolesDelete, Description: ""},
		}},
		{Resource: "integrations", Permissions: []PermissionMeta{
			{Name: PermIntegrationsRead, Description: ""},
			{Name: PermIntegrationsConnect, Description: ""},
			{Name: PermIntegrationsDisconnect, Description: ""},
		}},
		{Resource: "content", Permissions: []PermissionMeta{
			{Name: PermContentRead, Description: ""},
			{Name: PermContentCreate, Description: ""},
			{Name: PermContentUpdate, Description: ""},
			{Name: PermContentDelete, Description: ""},
		}},
		{Resource: "billing", Permissions: []PermissionMeta{
			{Name: PermBillingRead, Description: ""},
			{Name: PermBillingUpdate, Description: ""},
		}},
	}
}
