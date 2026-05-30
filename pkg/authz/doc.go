// Package authz exposes the OneVoice v2.0 RBAC primitives shared by the API
// gateway and downstream callers.
//
// # Phase Boundary
//
// ships:
// - permissions.go — typed Permission constants and AllPermissions accessor
// consumed by the GET /api/v1/permissions handler and the system-role
// drift integration test.
// - invariants.go  — EnsureOwnerExistsAfter, CheckEscalationSubset,
// CheckSelfLockout, and CheckSystemRoleImmutable free functions.
// - errors.go      — ErrLastOwner, ErrCannotGrantUnownedPermissions,
// ErrSelfLockout, ErrSystemRoleImmutable sentinel errors.
//
// adds (to this same package — CONTEXT decision):
// - cache.go    — Two-level LRU cache (membership 1024/30s, role 256/30s)
// - Invalidate API + NewCacheForTest TTL-injectable constructor.
// - check.go    — Can(ctx, perm) runtime check + BusinessContext +
// WithBusinessContext / BusinessContextFromCtx.
// - loader.go   — MembershipLoader interface (DI seam; impl in services/api/).
// - middleware.go — RequireBusinessAccess(cache, extractUserID) middleware.
//
// All callers import a single package path: github.com/f1xgun/onevoice/pkg/authz
//
// # Permission Format
//
// Flat resource.action strings only — no wildcards, no hierarchy. The
// migration seed (migrations/postgres/000007_rbac_data_model.up.sql and the
// integration-test mirror) hardcodes the same JSONB arrays; drift is caught
// by test/integration/system_roles_test.go which queries the seeded rows
// and asserts equality with AllPermissions minus role-specific
// exclusions.
package authz
