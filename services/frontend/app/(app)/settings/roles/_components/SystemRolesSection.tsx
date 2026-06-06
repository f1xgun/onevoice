'use client';

import { useTranslations } from 'next-intl';

import { Badge } from '@/components/ui/badge';
import { SYSTEM_ROLE_ORDER } from '@/lib/constants/roles';
import type { Role } from '@/lib/schemas';

import { RoleActionsMenu } from './RoleActionsMenu';

export interface SystemRolesSectionProps {
  roles: Role[];
  businessId: string | null;
}

export function SystemRolesSection({ roles, businessId }: SystemRolesSectionProps) {
  const t = useTranslations('roles.list');

  const sorted = [...roles].sort((a, b) => {
    const ai = SYSTEM_ROLE_ORDER.indexOf(a.name as (typeof SYSTEM_ROLE_ORDER)[number]);
    const bi = SYSTEM_ROLE_ORDER.indexOf(b.name as (typeof SYSTEM_ROLE_ORDER)[number]);
    return (
      (ai === -1 ? SYSTEM_ROLE_ORDER.length : ai) - (bi === -1 ? SYSTEM_ROLE_ORDER.length : bi)
    );
  });

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
