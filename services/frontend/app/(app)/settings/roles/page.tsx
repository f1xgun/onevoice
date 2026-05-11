'use client';

import { useTranslations } from 'next-intl';

import { Skeleton } from '@/components/ui/skeleton';
import { Button } from '@/components/ui/button';
import { RequirePermission } from '@/components/permission/RequirePermission';
import { useBusinessStore } from '@/lib/stores/business';
import { useRoles } from '@/lib/hooks/useRoles';

import { SystemRolesSection } from './_components/SystemRolesSection';
import { CustomRolesSection } from './_components/CustomRolesSection';

// /settings/roles — list entry for the Phase 5 custom-roles UI.
//
// The page is a Client Component because:
//   1. useRoles consumes React Query (client-side cache).
//   2. The two child sections wire DropdownMenu interactions + the
//      DeleteRoleDialog state machine.
//
// Wrapped in <RequirePermission perm="roles.read"> so an actor without
// the registry permission gets an empty render (the settings layout
// still surfaces — they can navigate elsewhere). This is a UX gate;
// the backend re-checks every GET on listRoles.

const SKELETON_SYSTEM_ROWS = 4;
const SKELETON_CUSTOM_ROWS = 2;

export default function RolesPage() {
  const t = useTranslations('roles');
  const tList = useTranslations('roles.list');
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const { data: roles, isLoading, isError, refetch } = useRoles(businessId);

  return (
    <RequirePermission perm="roles.read">
      <div className="space-y-8 px-4 pb-10 pt-6 sm:px-12 sm:pb-16">
        <h1 className="text-2xl font-semibold tracking-tight text-ink">{t('title')}</h1>

        {isLoading ? (
          <>
            <section className="space-y-3" aria-busy="true">
              <h2 className="text-lg font-medium text-ink">{tList('systemSection')}</h2>
              <ul
                role="list"
                className="divide-y divide-line rounded-md border border-line bg-paper-raised"
              >
                {Array.from({ length: SKELETON_SYSTEM_ROWS }).map((_, i) => (
                  <li key={i} className="flex items-center gap-4 px-4 py-3">
                    <Skeleton className="h-4 flex-1" />
                    <Skeleton className="h-4 w-20" />
                    <Skeleton className="h-5 w-16 rounded-full" />
                  </li>
                ))}
              </ul>
            </section>
            <section className="space-y-3" aria-busy="true">
              <h2 className="text-lg font-medium text-ink">{tList('customSection')}</h2>
              <ul
                role="list"
                className="divide-y divide-line rounded-md border border-line bg-paper-raised"
              >
                {Array.from({ length: SKELETON_CUSTOM_ROWS }).map((_, i) => (
                  <li key={i} className="flex items-center gap-4 px-4 py-3">
                    <Skeleton className="h-4 flex-1" />
                    <Skeleton className="h-4 w-24" />
                    <Skeleton className="h-4 w-24" />
                  </li>
                ))}
              </ul>
            </section>
          </>
        ) : isError ? (
          <div className="rounded-md border border-dashed border-line p-6">
            <p className="text-ink">{tList('loadError')}</p>
            <Button variant="secondary" size="sm" className="mt-3" onClick={() => void refetch()}>
              {tList('retry')}
            </Button>
          </div>
        ) : (
          (() => {
            const all = roles ?? [];
            const systemRoles = all.filter((r) => r.is_system);
            const customRoles = all.filter((r) => !r.is_system);
            return (
              <>
                <SystemRolesSection roles={systemRoles} businessId={businessId} />
                <CustomRolesSection roles={customRoles} businessId={businessId} allRoles={all} />
              </>
            );
          })()
        )}
      </div>
    </RequirePermission>
  );
}
