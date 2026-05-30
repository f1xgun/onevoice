'use client';

import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { getPermissionsCatalog } from '@/lib/api/permissions';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import type { PermissionGroup } from '@/lib/schemas';

/**
 * Lazy fetch of the static permission catalog (all permissions grouped by
 * resource, in the registry order from `pkg/authz.AllPermissions`).
 *
 * Mounted by `PermissionTree` on the role-editor pages — 99% of users never
 * open `/settings/roles`, so this fires only when an editor mounts (
 *
 * Cache discipline: `staleTime: Infinity` + `gcTime: Infinity` because the
 * catalog is static within a deploy. A new build/restart re-fetches at the
 * next mount of a fresh QueryClient (logout removes the cached entry; see
 * NavRail handleLogout → removeQueries(['permissions-catalog'])).
 */
export function usePermissionsCatalog(): UseQueryResult<PermissionGroup[]> {
  return useQuery<PermissionGroup[]>({
    queryKey: QUERY_KEYS.PERMISSIONS_CATALOG,
    queryFn: getPermissionsCatalog,
    staleTime: Infinity,
    gcTime: Infinity,
  });
}
