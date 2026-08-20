'use client';

import { useQuery } from '@tanstack/react-query';

import { getMyPermissions } from '@/lib/api/permissions';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';

/**
 * Result of a permission gate check.
 *
 * Reads from `GET /api/v1/businesses/{id}/me/permissions` via React Query.
 * `staleTime: 60_000` + `refetchInterval: 60_000` stays within the server-side
 * 30 s cache TTL, so eventual consistency caps at ~90 s.
 *
 * `allowed` reflects the last successfully loaded permission list; while the
 * list is loading — or failed to load — it stays `false`, which on its own is
 * indistinguishable from a real denial. Surfaces that lock controls behind
 * `allowed` must branch on `isError` and offer `refetch` instead of
 * presenting a load failure as "no permission".
 */
export interface PermissionResult {
  allowed: boolean;
  isLoading: boolean;
  /**
   * The permission list could not be loaded and there is no previously
   * loaded list to fall back on. A failed background refresh keeps the
   * last known list instead of raising this.
   */
  isError: boolean;
  refetch: () => void;
}

const PERM_REFRESH_INTERVAL_MS = 60_000;

export function usePermission(perm: string): PermissionResult {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const { data, isLoading, isLoadingError, refetch } = useQuery({
    queryKey: QUERY_KEYS.PERMISSIONS(activeBusinessId),
    queryFn: () => getMyPermissions(activeBusinessId as string),
    enabled: !!activeBusinessId,
    staleTime: PERM_REFRESH_INTERVAL_MS,
    refetchInterval: PERM_REFRESH_INTERVAL_MS,
    refetchIntervalInBackground: false,
    retry: 1,
  });
  return {
    allowed: data?.includes(perm) ?? false,
    isLoading,
    isError: isLoadingError,
    refetch,
  };
}
