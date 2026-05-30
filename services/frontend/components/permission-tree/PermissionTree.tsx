'use client';

import { useMemo } from 'react';

import { Skeleton } from '@/components/ui/skeleton';
import { usePermissionsCatalog } from '@/lib/hooks/usePermissionsCatalog';

import { GroupRow } from './GroupRow';

const SKELETON_GROUPS = 6;

export interface PermissionTreeProps {
  /** Currently-selected permission keys (controlled). */
  value: string[];
  /** Fires with the next array of permission keys after every user toggle. */
  onChange: (next: string[]) => void;
  /**
   * Set of permissions the actor (current user) holds. Used to disable leaves
   * the actor cannot grant (UI-RBAC-04 escalation-subset enforcement
   * is a backend concern; this is the UX affordance per docs/security.md).
   */
  actorPermissions: Set<string>;
  /** Form-level disable (e.g. during submit). Disables every checkbox. */
  disabled?: boolean;
}

/**
 * Tree of all permissions grouped by resource. Controlled component owned by
 * `RoleEditorForm`.
 *
 * Catalog is fetched lazily on first mount via React Query (staleTime: Infinity,
 * gcTime: Infinity ). 99% of users never see this tree, so no
 * app-boot waste.
 *
 * Locked behaviors:
 *  - : All groups expanded by default (no chevron-click needed).
 *  - : Group tri-state checkbox skips actor-disabled leaves on toggle —
 *     the tri-state is derived only over actor-enabled leaves so a partial
 *     actor never appears stuck on indeterminate.
 *  - : Each leaf renders Info icon + Tooltip with the permission's
 *     description (or «У вас нет этого права» if actor lacks it).
 *
 * UI-RBAC-11: every leaf key comes from `catalog[i].permissions[j].name` —
 * NO hardcoded permission strings in this file or its subtree.
 */
export function PermissionTree({
  value,
  onChange,
  actorPermissions,
  disabled = false,
}: PermissionTreeProps) {
  const { data: catalog, isLoading } = usePermissionsCatalog();
  const valueSet = useMemo(() => new Set(value), [value]);

  if (isLoading || !catalog) {
    return (
      <div className="space-y-2" aria-busy="true">
        {Array.from({ length: SKELETON_GROUPS }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-2">
      {catalog.map((group) => (
        <GroupRow
          key={group.resource}
          group={group}
          value={value}
          valueSet={valueSet}
          actorPermissions={actorPermissions}
          disabled={disabled}
          onChange={onChange}
        />
      ))}
    </div>
  );
}
