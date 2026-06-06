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
 */
export interface PermissionResult {
  allowed: boolean;
  isLoading: boolean;
}

const PERM_REFRESH_INTERVAL_MS = 60_000;

export function usePermission(perm: string): PermissionResult {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const { data, isLoading } = useQuery({
    queryKey: QUERY_KEYS.PERMISSIONS(activeBusinessId),
    queryFn: () => getMyPermissions(activeBusinessId as string),
    enabled: !!activeBusinessId,
    staleTime: PERM_REFRESH_INTERVAL_MS,
    refetchInterval: PERM_REFRESH_INTERVAL_MS,
    refetchIntervalInBackground: false,
    retry: 1,
  });
  if (isLoading) return { allowed: false, isLoading: true };
  return { allowed: data?.includes(perm) ?? false, isLoading: false };
}
