'use client';

import { useQuery } from '@tanstack/react-query';

import { getMyPermissions } from '@/lib/api/permissions';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';

/**
 * Result of a permission gate check.
 *
 * - `allowed` — the actor holds `perm` in the active business.
 * - `isLoading` — the per-business permissions query has not settled yet.
 *
 * Phase 5: body swapped to read from
 * `GET /api/v1/businesses/{id}/me/permissions` via React Query. Cache cadence:
 * `staleTime: 60_000` + `refetchInterval: 60_000` (CONTEXT D-06; matches
 * UI-RBAC-12 verbatim and stays within AUTHZ-03's 30 s server-cache TTL —
 * eventual consistency caps at ~90 s).
 *
 * Signature locked by Phase 4 D-05 — call sites do NOT change between phases.
 * The Phase 4 legacy hardcoded role→perms map is deleted in the same plan;
 * the registry is now the backend authority (UI-RBAC-11).
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
    // Phase 5 D-06: only poll while the tab is foreground. A backgrounded
    // dashboard does not need to renew perms — the next focus/refetch on
    // window focus will pick up the latest state.
    refetchIntervalInBackground: false,
    // NIT-02 (Phase 5 review): retry: 1 is INTENTIONAL — one retry is plenty.
    // Business-switch races and brief network blips recover, but a hard 5xx
    // surfacing as "allowed: false, isLoading: false" is the correct
    // fail-closed posture for a permission gate. Tuning higher would double
    // log noise during real outages without improving UX (the per-business
    // PermissionsCacheGuard already absorbs the first retry). The
    // axios interceptor does NOT add a retry layer (lib/api.ts) — verified
    // to avoid compounding retry counts.
    retry: 1,
  });
  if (isLoading) return { allowed: false, isLoading: true };
  return { allowed: data?.includes(perm) ?? false, isLoading: false };
}
