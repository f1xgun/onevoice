'use client';

import { EmptyFrame } from '@/components/states/EmptyFrame';
import { EmptyAction } from '@/components/states/EmptyAction';

import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';

import { useLocale, useTranslations } from 'next-intl';
import type { AuditLogDTO } from '../_lib/types';
import { actionToI18nKey, isKnownAuditAction } from '../_lib/actionLabels';
import { isKnownResource } from '../_lib/resourceLabels';
import { Skeleton } from '@/components/ui/skeleton';
import { ActionButton as Button } from '@/components/design-system/ActionButton';

interface Props {
  items: AuditLogDTO[];
  isLoading: boolean;
  hasNextPage: boolean;
  isFetchingMore: boolean;
  onLoadMore: () => void;
  onResetFilters?: () => void;
  isRefreshing?: boolean;
  onRowClick: (item: AuditLogDTO) => void;
}

const SKELETON_ROW_COUNT = 5;
const TIME_FORMAT: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' };
type AuditFiltersTranslator = ReturnType<typeof useTranslations<'audit.filters'>>;

// SYSTEM_ACTIONS are emitted by the platform itself with no human actor by
// design (the audit builders leave user_id nil). Rendering them as "Система"
// is clearer than "Неизвестен (—)", which reads like a missing-data bug.
const SYSTEM_ACTIONS = new Set<string>([
  'integration.token_decrypted',
  'integration.token_rotated',
  'rpa.scope_violation',
  'review.auto_replied',
]);

function actorLabel(item: AuditLogDTO, tFilters: AuditFiltersTranslator): string {
  if (item.actor_display_name) return item.actor_display_name;
  if (item.actor_email) return item.actor_email;
  const d = item.details as Record<string, unknown> | null;
  if (d && typeof d === 'object' && typeof d.attempted_email === 'string') {
    return tFilters('actorUnknown', { email: d.attempted_email });
  }
  if (SYSTEM_ACTIONS.has(item.action)) {
    return tFilters('actorSystem');
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
  onResetFilters,
  isRefreshing = false,
}: Props) {
  const t = useTranslations('audit.table');
  const locale = useLocale();
  const tEmpty = useTranslations('states.actions');
  const tActions = useTranslations();
  const tFilters = useTranslations('audit.filters');
  const tResources = useTranslations('audit.resources');

  if ((isLoading || isRefreshing) && items.length === 0) {
    return (
      <LoadingPlaceholder data-testid="audit-table-loading" className="space-y-2" aria-busy="true">
        {Array.from({ length: SKELETON_ROW_COUNT }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </LoadingPlaceholder>
    );
  }

  if (items.length === 0) {
    return (
      <div data-testid="audit-table-empty">
        <EmptyFrame
          title={t('empty')}
          body={t('emptyBody')}
          action={
            <>
              <EmptyAction />
              {onResetFilters && (
                <Button variant="ghost" size="sm" onClick={onResetFilters}>
                  {tEmpty('resetFilters')}
                </Button>
              )}
            </>
          }
        />
      </div>
    );
  }

  return (
    <div className="space-y-3" aria-busy={isRefreshing || isFetchingMore}>
      {Array.from(
        new Set(items.map((item) => new Date(item.created_at).toLocaleDateString(locale)))
      ).map((day) => (
        <section key={day}>
          <h3 className="mb-block-gap text-group-title">{day}</h3>
          <ul className="rounded-block bg-block p-1 md:p-2">
            {items
              .filter((item) => new Date(item.created_at).toLocaleDateString(locale) === day)
              .map((it) => (
                <li key={it.id}>
                  <button
                    data-testid={`audit-row-${it.id}`}
                    type="button"
                    onClick={() => onRowClick(it)}
                    className="flex min-h-tap w-full items-start gap-3 rounded-inner p-3 text-left hover:bg-inset"
                  >
                    <span
                      aria-hidden
                      className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-avatar text-meta text-avatar-ink"
                    >
                      {actorLabel(it, tFilters).slice(0, 1)}
                    </span>
                    <span className="min-w-0 flex-1 break-words text-reading">
                      <span className="font-medium">{actorLabel(it, tFilters)}</span>{' '}
                      {isKnownAuditAction(it.action)
                        ? tActions(actionToI18nKey(it.action))
                        : tActions('audit.actions.unknown')}
                      <span className="block text-meta text-ink-soft">
                        {isKnownResource(it.resource)
                          ? tResources(it.resource)
                          : tResources('other')}
                      </span>
                    </span>
                    <time
                      dateTime={it.created_at}
                      title={new Date(it.created_at).toLocaleString(locale)}
                      className="shrink-0 text-meta text-ink-soft"
                    >
                      {new Date(it.created_at).toLocaleTimeString(locale, TIME_FORMAT)}
                    </time>
                  </button>
                </li>
              ))}
          </ul>
        </section>
      ))}
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
