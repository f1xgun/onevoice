'use client';

import { useTranslations } from 'next-intl';
import type { AuditLogDTO } from '../_lib/types';
import { actionToI18nKey } from '../_lib/actionLabels';
import { Skeleton } from '@/components/ui/skeleton';
import { Button } from '@/components/ui/button';

interface Props {
  items: AuditLogDTO[];
  isLoading: boolean;
  hasNextPage: boolean;
  isFetchingMore: boolean;
  onLoadMore: () => void;
  onRowClick: (item: AuditLogDTO) => void;
}

const MS_PER_SEC = 1000;
const SEC_PER_MIN = 60;
const MIN_PER_HOUR = 60;
const HOURS_PER_DAY = 24;
const SKELETON_ROW_COUNT = 5;
const LOCALE = 'ru-RU';

type AuditTableTranslator = ReturnType<typeof useTranslations<'audit.table'>>;
type AuditFiltersTranslator = ReturnType<typeof useTranslations<'audit.filters'>>;

function formatRelative(iso: string, tTable: AuditTableTranslator): string {
  const ms = Date.now() - new Date(iso).getTime();
  const sec = Math.floor(ms / MS_PER_SEC);
  if (sec < SEC_PER_MIN) return tTable('relJustNow');
  const min = Math.floor(sec / SEC_PER_MIN);
  if (min < MIN_PER_HOUR) return tTable('relMinutes', { m: min });
  const hr = Math.floor(min / MIN_PER_HOUR);
  if (hr < HOURS_PER_DAY) return tTable('relHours', { h: hr });
  const d = Math.floor(hr / HOURS_PER_DAY);
  return tTable('relDays', { d });
}

function actorLabel(item: AuditLogDTO, tFilters: AuditFiltersTranslator): string {
  if (item.actor_display_name) return item.actor_display_name;
  if (item.actor_email) return item.actor_email;
  const d = item.details as Record<string, unknown> | null;
  if (d && typeof d === 'object' && typeof d.attempted_email === 'string') {
    return tFilters('actorUnknown', { email: d.attempted_email });
  }
  return tFilters('actorUnknown', { email: '—' });
}

export function AuditTable({
  items,
  isLoading,
  hasNextPage,
  isFetchingMore,
  onLoadMore,
  onRowClick,
}: Props) {
  const t = useTranslations('audit.table');
  const tActions = useTranslations();
  const tFilters = useTranslations('audit.filters');

  if (isLoading && items.length === 0) {
    return (
      <div data-testid="audit-table-loading" className="space-y-2" aria-busy="true">
        {Array.from({ length: SKELETON_ROW_COUNT }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <p data-testid="audit-table-empty" className="py-12 text-center text-ink-soft">
        {t('empty')}
      </p>
    );
  }

  return (
    <div className="space-y-3">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-line text-left text-ink-soft">
              <th className="py-2 pr-4 font-medium">{t('colTime')}</th>
              <th className="py-2 pr-4 font-medium">{t('colActor')}</th>
              <th className="py-2 pr-4 font-medium">{t('colAction')}</th>
              <th className="py-2 pr-4 font-medium">{t('colResource')}</th>
            </tr>
          </thead>
          <tbody>
            {items.map((it) => (
              <tr
                key={it.id}
                data-testid={`audit-row-${it.id}`}
                onClick={() => onRowClick(it)}
                className="cursor-pointer border-b border-line text-ink hover:bg-paper-sunken"
              >
                <td className="py-2 pr-4" title={new Date(it.created_at).toLocaleString(LOCALE)}>
                  {formatRelative(it.created_at, t)}
                </td>
                <td className="py-2 pr-4">{actorLabel(it, tFilters)}</td>
                <td className="py-2 pr-4">{tActions(actionToI18nKey(it.action))}</td>
                <td className="py-2 pr-4">{it.resource}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {hasNextPage ? (
        <div className="flex justify-center">
          <Button type="button" variant="secondary" onClick={onLoadMore} disabled={isFetchingMore}>
            {isFetchingMore ? t('loading') : t('loadMore')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
