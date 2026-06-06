'use client';

import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { Plus } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { RequirePermission } from '@/components/permission/RequirePermission';
import type { Role } from '@/lib/schemas';

import { RoleActionsMenu } from './RoleActionsMenu';

export interface CustomRolesSectionProps {
  roles: Role[];
  businessId: string | null;
  // Full role list forwarded into DeleteRoleDialog's reassign picker so
  // every candidate target is available without re-fetching.
  allRoles: Role[];
}

export function CustomRolesSection({ roles, businessId, allRoles }: CustomRolesSectionProps) {
  const t = useTranslations('roles.list');

  const sorted = [...roles].sort((a, b) => a.name.localeCompare(b.name, 'ru'));

  return (
    <section className="space-y-3" aria-labelledby="roles-custom-heading">
      <div className="flex items-center justify-between">
        <h2 id="roles-custom-heading" className="text-lg font-medium text-ink">
          {t('customSection')}
        </h2>
        {/* CTA gated by roles.create — actor without create perm shouldn't see */}
        {/* the affordance at all (UI gate; backend re-checks on POST). */}
        <RequirePermission perm="roles.create">
          <Button asChild size="sm" variant="primary" className="gap-1">
            <Link href="/settings/roles/new" aria-label={t('addRole')}>
              <Plus className="h-4 w-4" aria-hidden />
              {t('addRole')}
            </Link>
          </Button>
        </RequirePermission>
      </div>

      {sorted.length === 0 ? (
        <div className="rounded-md border border-dashed border-line p-6 text-center">
          <p className="text-ink">{t('noCustom')}</p>
          <p className="mt-1 text-sm text-ink-soft">{t('noCustomCta')}</p>
        </div>
      ) : (
        <ul
          role="list"
          className="divide-y divide-line rounded-md border border-line bg-paper-raised"
        >
          {sorted.map((role) => (
            <li key={role.id} className="relative">
              <Link
                href={`/settings/roles/${role.id}/edit`}
                aria-label={role.name}
                className="flex items-center gap-4 px-4 py-3 text-ink transition-colors hover:bg-paper-sunken focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span className="flex-1 font-medium">{role.name}</span>
                <span className="text-sm text-ink-soft">
                  {t('permsCount', { count: role.permissions.length })}
                </span>
                <span className="text-sm text-ink-soft">
                  {t('membersCount', { count: role.member_count ?? 0 })}
                </span>
                {/* Spacer so the menu trigger has room next to it; the menu */}
                {/* itself is rendered as a sibling absolutely positioned to */}
                {/* avoid nesting a button inside an anchor. */}
                <span className="w-9" aria-hidden />
              </Link>
              {/*
                Keep this sibling OUTSIDE the <Link>. A <button> nested in an
                <a> is invalid HTML AND browsers fire the anchor's click handler
                before the button's, so the row navigates BEFORE the menu opens
                (stopPropagation inside RoleActionsMenu does not save you
                because the event originates in the descendant menu, not the
                anchor).
              */}
              <span className="absolute inset-y-0 right-3 flex items-center">
                <RoleActionsMenu role={role} businessId={businessId} allRoles={allRoles} />
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
