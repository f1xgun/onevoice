'use client';

import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';

/**
 * Mount once inside `(app)/layout.tsx`. Invalidates the per-business
 * permissions cache when `activeBusinessId` changes — belt-and-suspenders
 * defense against same-id-reselect on top of the per-business `useQuery` key.
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
