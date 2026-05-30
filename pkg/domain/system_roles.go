// Package domain — system_roles.go
//
// SystemRole*ID values are the deterministic UUIDs seeded by the v2.0 RBAC
// migration (migrations/postgres/000007_rbac_data_model.up.sql + the
// integration-test mirror at services/api/migrations/000005_rbac_data_model.up.sql).
// Hard-coding them in Go keeps backfill, repository inserts, the BEFORE DELETE
// trigger on users, and integration tests aligned with the seed without an
// extra DB round-trip.
//
// "Owner" for the last-owner invariant (AUTHZ-06) is defined as
// "member with role_id = SystemRoleOwnerID" — NOT "member with all
// permissions" — per CONTEXT decision. System roles are immutable
// (is_system=true); enforcement of that immutability is application-side
// in Phases 2/5.
package domain

// System role UUIDs — must match the seed in migration 000007/000005. Drift
// between this file and the seeded JSONB is caught by
// test/integration/system_roles_test.go (Plan H).
const (
	SystemRoleOwnerID  = "00000000-0000-0000-0000-000000000001"
	SystemRoleAdminID  = "00000000-0000-0000-0000-000000000002"
	SystemRoleEditorID = "00000000-0000-0000-0000-000000000003"
	SystemRoleViewerID = "00000000-0000-0000-0000-000000000004"
)
