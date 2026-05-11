import { api } from '@/lib/api';
import { bizApi } from '@/lib/api/business-api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { myPermissionsSchema, permissionsCatalogSchema, type PermissionGroup } from '@/lib/schemas';

// Backend contracts (Phase 5, Plan 05-03):
//   GET /api/v1/permissions
//     → 200 { groups: [{ resource, permissions: [{name, description}] }] }
//   GET /api/v1/businesses/{id}/me/permissions
//     → 200 { permissions: string[] }
//
// The catalog is top-level (not business-scoped) — it's an app-static registry
// shared across every tenant. The "me" endpoint is per-business: the cache
// entry partitions by activeBusinessId so a business switch naturally drops
// the previous tenant's perms.

/**
 * Fetches the static permission registry (catalog of all permissions grouped
 * by resource). The wire shape is `{ groups: PermissionGroup[] }` — this
 * function unwraps and returns the groups array. Cache via React Query at
 * the call site with `staleTime: Infinity` (it's app-static).
 */
export async function getPermissionsCatalog(): Promise<PermissionGroup[]> {
  const { data } = await api.get<unknown>(API_PATHS.PERMISSIONS);
  return permissionsCatalogSchema.parse(data).groups;
}

/**
 * Fetches the actor's effective permissions in `businessId`. Returns a flat
 * `string[]`. The owner role enumerates every permission (no `*` sentinel on
 * the wire per Phase 5 D-05).
 */
export async function getMyPermissions(businessId: string): Promise<string[]> {
  const { data } = await bizApi(businessId).get<unknown>(BIZ_API_PATHS.ME.PERMISSIONS);
  return myPermissionsSchema.parse(data).permissions;
}
