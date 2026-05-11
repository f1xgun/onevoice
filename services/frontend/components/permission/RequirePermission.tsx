'use client';

import type { ReactNode } from 'react';
import { usePermission } from '@/lib/hooks/usePermission';

export interface RequirePermissionProps {
  /** Flat permission string, e.g. "members.invite". Must match keys in PERMISSIONS_BY_ROLE. */
  perm: string;
  /** Rendered when the active role lacks `perm`. Default: `null` (hide entirely). */
  fallback?: ReactNode;
  children: ReactNode;
}

/**
 * UI-only permission gate. Hides children when the active role lacks `perm`.
 * NOT a security boundary — the backend re-checks every mutation.
 * Use to prevent showing destructive buttons that would 403 anyway.
 */
export function RequirePermission({ perm, fallback = null, children }: RequirePermissionProps) {
  const { allowed } = usePermission(perm);
  return <>{allowed ? children : fallback}</>;
}
