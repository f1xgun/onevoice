'use client';

import { useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';

import { RequirePermission } from '@/components/permission/RequirePermission';
import { useBusinessStore } from '@/lib/stores/business';
import { useAuditLogs } from './_lib/useAuditLogs';
import type { AuditFilters as TFilters, AuditLogDTO } from './_lib/types';
import { AuditFilters } from './_components/AuditFilters';
import { AuditTable } from './_components/AuditTable';
import { AuditDetailPanel } from './_components/AuditDetailPanel';

const DEFAULT_WINDOW_DAYS = 7;
const MS_PER_SEC = 1000;
const SEC_PER_MIN = 60;
const MIN_PER_HOUR = 60;
const HOURS_PER_DAY = 24;
const MS_PER_DAY = HOURS_PER_DAY * MIN_PER_HOUR * SEC_PER_MIN * MS_PER_SEC;

// /settings/audit — owner/admin-only audit log explorer.
//
// The page is a Client Component because:
//   1. useAuditLogs consumes TanStack useInfiniteQuery (client-side cache).
//   2. Filter state + side-panel selection are local-only and never need
//      server hydration — round-tripping them through search params would
//      add latency without UX benefit for v1.
//
// Wrapped in <RequirePermission perm="audit.read"> so an actor without
// the audit.read permission gets the explicit error message (the
// settings layout still renders — they can navigate elsewhere). The
// backend re-checks every GET.
function defaultFilters(): TFilters {
  const now = new Date();
  const from = new Date(now.getTime() - DEFAULT_WINDOW_DAYS * MS_PER_DAY);
  return { category: 'all', from: from.toISOString(), to: now.toISOString() };
}

export default function AuditPage() {
  const t = useTranslations('audit');
  const businessID = useBusinessStore((s) => s.activeBusinessId);
  const [filters, setFilters] = useState<TFilters>(defaultFilters);
  const [selected, setSelected] = useState<AuditLogDTO | null>(null);

  const { items, hasNextPage, fetchNextPage, isLoading, isFetchingNextPage, error } = useAuditLogs(
    businessID,
    filters
  );

  const errorMessage = useMemo(() => {
    if (!error) return null;
    if (error.code === 'forbidden') return t('errors.forbidden');
    return t('errors.loadFailed');
  }, [error, t]);

  return (
    <RequirePermission
      perm="audit.read"
      fallback={
        <div className="px-4 pb-10 pt-6 sm:px-12 sm:pb-16">
          <h1 className="text-2xl font-semibold tracking-tight text-ink">{t('title')}</h1>
          <p className="mt-4 rounded-md border border-line bg-paper-sunken p-3 text-ink">
            {t('errors.forbidden')}
          </p>
        </div>
      }
    >
      <section className="space-y-6 px-4 pb-10 pt-6 sm:px-12 sm:pb-16">
        <header className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight text-ink">{t('title')}</h1>
          <p className="text-ink-soft">{t('subtitle')}</p>
        </header>

        {businessID ? (
          <AuditFilters value={filters} onChange={setFilters} businessID={businessID} />
        ) : null}

        {errorMessage ? (
          <div role="alert" className="rounded-md border border-destructive p-3 text-destructive">
            {errorMessage}
          </div>
        ) : null}

        <AuditTable
          items={items}
          isLoading={isLoading}
          hasNextPage={!!hasNextPage}
          isFetchingMore={isFetchingNextPage}
          onLoadMore={() => {
            void fetchNextPage();
          }}
          onRowClick={setSelected}
        />

        <AuditDetailPanel item={selected} onClose={() => setSelected(null)} />
      </section>
    </RequirePermission>
  );
}
