// System role seniority order. The catalog is locked at the backend
// (system roles owner / admin / editor / viewer), and several role-
// management surfaces sort by this order so the UI reads
// "most powerful → least powerful" top-down. Shared so the two surfaces
// (SystemRolesSection and DeleteRoleDialog) can't drift.
export const SYSTEM_ROLE_ORDER = ['owner', 'admin', 'editor', 'viewer'] as const;

export type SystemRoleName = (typeof SYSTEM_ROLE_ORDER)[number];
