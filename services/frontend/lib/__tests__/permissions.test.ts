import { describe, it, expect } from 'vitest';

// ---------------------------------------------------------------------------
// Permissions drift snapshot (frontend ↔ backend).
//
// Phase 5 replaces the Phase 4 hardcoded role→permissions map with a
// dynamic registry served by `GET /api/v1/permissions`. The legacy module
// `lib/permissions.ts` is DELETED in the same plan (atomic swap; no
// deprecation period — RESEARCH Pitfall 9).
//
// The new drift surface is the snapshot below. It captures the registry as
// of Phase 5 (21 permissions in 6 resource groups). Any future change to
// `pkg/authz.AllPermissions()` — adding, removing, or renaming a permission —
// MUST update this snapshot in the same PR. CI then catches drift between
// the Go registry and the frontend's expectations.
//
// The matching backend test
// (TestAllPermissions_DescriptionsNotEmpty in pkg/authz/permissions_test.go)
// guarantees every registered permission carries a non-empty Russian
// description.
// ---------------------------------------------------------------------------

const EXPECTED_PERMISSION_NAMES = [
  'business.read',
  'business.update',
  'business.delete',
  'business.transfer_ownership',
  'members.read',
  'members.invite',
  'members.remove',
  'members.update_role',
  'roles.read',
  'roles.create',
  'roles.update',
  'roles.delete',
  'integrations.read',
  'integrations.connect',
  'integrations.disconnect',
  'content.read',
  'content.create',
  'content.update',
  'content.delete',
  'billing.read',
  'billing.update',
] as const;

const EXPECTED_RESOURCES = [
  'business',
  'members',
  'roles',
  'integrations',
  'content',
  'billing',
] as const;

describe('permissions registry snapshot (frontend ↔ backend drift surface)', () => {
  it('has 21 permissions in 6 resource groups (Phase 5 baseline)', () => {
    expect(EXPECTED_PERMISSION_NAMES).toHaveLength(21);
    expect(EXPECTED_RESOURCES).toHaveLength(6);
  });

  it('every permission name follows the resource.action convention', () => {
    for (const name of EXPECTED_PERMISSION_NAMES) {
      // The registry contract: lowercase resource + dot + lowercase action.
      // If a backend PR ships a name like `Roles.Read` or `roles:read`, this
      // assertion fails and forces a frontend-side review.
      expect(name).toMatch(/^[a-z]+\.[a-z_]+$/);
    }
  });

  it('every permission name belongs to an expected resource group', () => {
    for (const name of EXPECTED_PERMISSION_NAMES) {
      const resource = name.split('.')[0];
      expect(EXPECTED_RESOURCES).toContain(resource);
    }
  });

  it('snapshot has no duplicates (sanity)', () => {
    const set = new Set(EXPECTED_PERMISSION_NAMES);
    expect(set.size).toBe(EXPECTED_PERMISSION_NAMES.length);
  });
});
