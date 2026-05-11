'use client';

import type { ReactNode } from 'react';
import { usePermission } from '@/lib/hooks/usePermission';

export interface RequirePermissionProps {
  /**
   * Flat permission string, e.g. `"members.invite"`. Must match a name returned
   * by `GET /api/v1/permissions` (the dynamic registry — Plan 05-04).
   */
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
