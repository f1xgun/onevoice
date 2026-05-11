'use client';

import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';

/**
 * Mount once inside `(app)/layout.tsx`. Invalidates the per-business
 * permissions cache when `activeBusinessId` changes (CONTEXT D-07 +
 * RESEARCH Pitfall 4 — reaction-not-side-effect pattern: the Zustand
 * reducer stays pure; the invalidate lives in a React layout effect).
 *
 * The `useQuery` key in `usePermission` already changes when
 * `activeBusinessId` changes (the cache entry is per-business), so this
 * guard is primarily a belt-and-suspenders defense against same-id-reselect
 * AND a visible signal that UI-RBAC-12 ("refresh on every business switch")
 * is implemented.
 *
 * Renders no DOM; sole purpose is the effect.
 */
export function PermissionsCacheGuard(): null {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const qc = useQueryClient();
  useEffect(() => {
    if (activeBusinessId) {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.PERMISSIONS(activeBusinessId) });
    }
  }, [activeBusinessId, qc]);
  return null;
}
