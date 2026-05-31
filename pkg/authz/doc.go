// Package authz exposes the OneVoice RBAC primitives shared by the API
// gateway and downstream callers.
//
// Files:
//   - permissions.go — typed Permission constants and AllPermissions accessor.
//   - invariants.go  — EnsureOwnerExistsAfter, CheckEscalationSubset,
//     CheckSelfLockout, CheckSystemRoleImmutable.
//   - errors.go      — sentinel errors (ErrLastOwner, ErrCannotGrantUnownedPermissions,
//     ErrSelfLockout, ErrSystemRoleImmutable).
//   - cache.go       — two-level LRU cache (membership 1024/30s, role 256/30s),
//     Invalidate API, NewCacheForTest TTL-injectable constructor.
//   - check.go       — Can(ctx, perm) runtime check + BusinessContext helpers.
//   - loader.go      — MembershipLoader interface (DI seam; impl in services/api/).
//   - middleware.go  — RequireBusinessAccess(cache, extractUserID) middleware.
//
// # Permission Format
//
// Flat resource.action strings only — no wildcards, no hierarchy. The
// migration seed (migrations/postgres/000007_rbac_data_model.up.sql and the
// integration-test mirror) hardcodes the same JSONB arrays; drift is caught
// by test/integration/system_roles_test.go which queries the seeded rows
// and asserts equality with AllPermissions minus role-specific exclusions.
package authz
