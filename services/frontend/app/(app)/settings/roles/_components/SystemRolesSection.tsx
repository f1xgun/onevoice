'use client';

import { useTranslations } from 'next-intl';

import { Badge } from '@/components/ui/badge';
import type { Role } from '@/lib/schemas';

import { RoleActionsMenu } from './RoleActionsMenu';

// Fixed order for system roles (D-03). The system catalog is locked at the
// backend (Plan 05-03) — we render them in seniority order so the table
// reads "most powerful → least powerful" top-down, matching the UI-SPEC.
const SYSTEM_ORDER = ['owner', 'admin', 'editor', 'viewer'] as const;

export interface SystemRolesSectionProps {
  roles: Role[];
  businessId: string | null;
}

export function SystemRolesSection({ roles, businessId }: SystemRolesSectionProps) {
  const t = useTranslations('roles.list');

  // Sort by seniority. Roles outside SYSTEM_ORDER (defensive) get pushed
  // to the end so the UI never blows up if a new system role ships before
  // the constant is updated.
  const sorted = [...roles].sort((a, b) => {
    const ai = SYSTEM_ORDER.indexOf(a.name as (typeof SYSTEM_ORDER)[number]);
    const bi = SYSTEM_ORDER.indexOf(b.name as (typeof SYSTEM_ORDER)[number]);
    return (ai === -1 ? SYSTEM_ORDER.length : ai) - (bi === -1 ? SYSTEM_ORDER.length : bi);
  });

  // Owner is the only role guaranteed to hold every permission (Plan 05-03);
  // showing "12 прав" for owner would just visually clutter what is conceptually
  // "everything". The localized "все права" badge replaces the count.
  const isOwner = (r: Role) => r.name === 'owner';

  return (
    <section className="space-y-3" aria-labelledby="roles-system-heading">
      <h2 id="roles-system-heading" className="text-lg font-medium text-ink">
        {t('systemSection')}
      </h2>
      <ul
        role="list"
        className="divide-y divide-line rounded-md border border-line bg-paper-raised"
      >
        {sorted.map((role) => {
          const permsLabel = isOwner(role)
            ? t('allPerms')
            : t('permsCount', { count: role.permissions.length });
          return (
            <li
              key={role.id}
              className="flex cursor-default items-center gap-4 px-4 py-3 opacity-60"
              aria-label={role.name}
            >
              <span className="flex-1 font-medium capitalize text-ink">{role.name}</span>
              <span className="text-sm text-ink-soft">{permsLabel}</span>
              <Badge tone="neutral">{t('systemBadge')}</Badge>
              <RoleActionsMenu role={role} businessId={businessId} />
            </li>
          );
        })}
      </ul>
    </section>
  );
}
