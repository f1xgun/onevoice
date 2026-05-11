'use client';

import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { useBusinessStore } from '@/lib/stores/business';
import { roleHasPermission } from '@/lib/permissions';

export interface PermissionResult {
  allowed: boolean;
  isLoading: boolean;
}

/**
 * Returns whether the active business's role grants `perm`.
 *
 * Phase 4 reads from the static {@link PERMISSIONS_BY_ROLE} table.
 * Phase 5 swaps the body to consume `GET /api/v1/permissions` — call sites
 * do NOT change (the signature is the contract).
 *
 * Returns `{ allowed: false, isLoading: true }` while the business list is
 * still loading; `{ allowed: false, isLoading: false }` if no business is
 * active or the role is unknown (e.g. custom roles in Phase 5).
 */
export function usePermission(perm: string): PermissionResult {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const { data: businesses, isLoading } = useBusinessList();

  if (isLoading) return { allowed: false, isLoading: true };

  const active = businesses?.find((b) => b.id === activeBusinessId);
  const roleName = active?.role.name;

  return { allowed: roleHasPermission(roleName, perm), isLoading: false };
}
