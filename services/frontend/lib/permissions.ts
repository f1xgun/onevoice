/**
 * Static role→permissions map for Phase 4 UI gates.
 *
 * Source of truth: migrations/postgres/000006_rbac_data_model.up.sql lines 70–101
 * (the JSONB `permissions` column of each system role row).
 *
 * The drift test in `lib/__tests__/permissions.test.ts` keeps this table in
 * sync with the DB seed.
 *
 * Owner uses the `'*'` sentinel — the consumer expands it via
 * `roleSet.includes('*') || roleSet.includes(perm)`. This avoids drift when
 * Phase 1 adds a new permission to the registry but the owner-everywhere
 * convention still holds.
 *
 * Phase 5 will replace this map with a registry fetched from
 * `GET /api/v1/permissions` per UI-RBAC-11. The hook signature is stable
 * (see `usePermission.ts`); call sites do not change.
 */
export const PERMISSIONS_BY_ROLE: Record<string, readonly string[]> = {
  owner: ['*'],
  admin: [
    'business.read',
    'business.update',
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
  ],
  editor: [
    'business.read',
    'members.read',
    'roles.read',
    'integrations.read',
    'integrations.connect',
    'integrations.disconnect',
    'content.read',
    'content.create',
    'content.update',
    'content.delete',
  ],
  viewer: [
    'business.read',
    'members.read',
    'roles.read',
    'integrations.read',
    'content.read',
    'billing.read',
  ],
} as const;

export const OWNER_SENTINEL = '*' as const;

export function roleHasPermission(roleName: string | undefined, perm: string): boolean {
  if (!roleName) return false;
  const list = PERMISSIONS_BY_ROLE[roleName];
  if (!list) return false;
  return list.includes(OWNER_SENTINEL) || list.includes(perm);
}
