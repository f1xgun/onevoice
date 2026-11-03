'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import { CheckCircle2 } from 'lucide-react';
import { toast } from 'sonner';
import {
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from '@/components/design-system/AppAlertDialog';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { PlatformIcon } from './PlatformIcons';
import { DriftAlertSettings } from './DriftAlertSettings';
import { useIntegrationsDrift, useVerifyIntegrations } from '@/lib/hooks/useIntegrationsDrift';
import {
  adoptIntegrationHours,
  restoreIntegrationHours,
  type IntegrationDrift,
} from '@/lib/api/integrationsSync';
import { usePermission } from '@/lib/hooks/usePermission';
import { usePlatformMeta, type PlatformId } from '@/lib/platforms';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { decisionQueueKey } from '@/hooks/useDecisionQueue';
import { scheduleDifferences, hasCompleteYandexHours } from '@/lib/decision-display';
import { openingHours } from '@/lib/business-schedule';
import { formatClockTime } from '@/lib/time-display';
import { getIntegrationDisplay } from '@/lib/integrations';
import { isAxiosError } from 'axios';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';

interface Integration {
  id: string;
  platform: string;
  externalId: string;
  metadata?: Record<string, unknown>;
}
interface Props {
  businessId: string;
  integrations: Integration[];
  repairLabel?: string;
  showAlertSettings?: boolean;
}
type SyncStatus = 'in_sync' | 'out_of_sync' | 'not_checked';
const KNOWN_FIELDS = new Set(['title', 'description', 'website', 'schedule']);

export function syncStatusFor(entry: IntegrationDrift | undefined): SyncStatus {
  if (!entry?.lastCheckedAt || entry.checkFailed) return 'not_checked';
  return entry.driftDetected ? 'out_of_sync' : 'in_sync';
}

export function IntegrationsSyncPanel({
  businessId,
  integrations,
  showAlertSettings = false,
}: Props) {
  const t = useTranslations('integrations.comparison');
  const fields = useTranslations('integrations.sync.fields');
  const days = useTranslations('business.daysOfWeek');
  const locale = useLocale();
  const metadata = usePlatformMeta();
  const permission = usePermission('business.update');
  const canEdit = permission.allowed && !permission.isLoading && !permission.isError;
  const query = useIntegrationsDrift(businessId);
  const qc = useQueryClient();
  const verify = useVerifyIntegrations(businessId);
  const restore = useMutation({
    mutationFn: () => restoreIntegrationHours(businessId),
    onSuccess: () => {
      toast.success(t('restoreStarted'));
      void query.refetch();
    },
    onError: () => toast.error(t('failed')),
  });
  const adopt = useMutation({
    mutationFn: (entry: IntegrationDrift) =>
      adoptIntegrationHours(businessId, entry.externalId, entry.lastCheckedAt!),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE(businessId) });
      void qc.invalidateQueries({ queryKey: decisionQueueKey(businessId) });
      void query.refetch();
      toast.success(t('adopted'));
    },
    onError: (error) => {
      toast.error(
        t(
          isAxiosError(error) && error.response?.status === HTTP_STATUS.CONFLICT
            ? 'changed'
            : 'failed'
        )
      );
      void query.refetch();
    },
  });
  const entries = integrations.map((integration) => ({
    integration,
    entry: query.data?.find(
      (row) => row.platform === integration.platform && row.externalId === integration.externalId
    ),
  }));
  const mismatches = entries.filter(
    ({ entry }) =>
      syncStatusFor(entry) === 'out_of_sync' &&
      entry?.driftFields.some((field) => KNOWN_FIELDS.has(field))
  );
  const allMatch =
    entries.length > 0 && entries.every(({ entry }) => syncStatusFor(entry) === 'in_sync');
  const checkedAt = entries
    .map(({ entry }) => entry?.lastCheckedAt)
    .filter((value): value is string => !!value)
    .sort()[0];
  const name = (integration: Integration) =>
    metadata[integration.platform as PlatformId]?.fullLabel || t('platform');
  const genericRepair = () =>
    verify.mutate(undefined, {
      onSuccess: () => toast.success(t('restoreStarted')),
      onError: () => toast.error(t('failed')),
    });
  if (integrations.length === 0) return null;
  if (query.isLoading)
    return (
      <LoadingPlaceholder role="status" className="h-12 rounded-block" aria-label={t('loading')} />
    );
  if (query.isError) return <ListLoadError onRetry={() => void query.refetch()} />;
  return (
    <div className="space-y-3">
      {allMatch && (
        <div className="flex items-start gap-2 text-[15px] leading-[22px]">
          <CheckCircle2 aria-hidden className="mt-0.5 h-4 w-4 shrink-0 text-ok" />
          <p className="flex-1">
            {t('matched', {
              platforms: new Intl.ListFormat(locale, { type: 'conjunction' }).format(
                integrations.map((integration) =>
                  t(
                    t.has(`with.${integration.platform}`)
                      ? `with.${integration.platform}`
                      : 'with.other'
                  )
                )
              ),
            })}
          </p>
          {checkedAt && (
            <time dateTime={checkedAt} className="shrink-0 text-meta text-ink-soft">
              {formatClockTime(checkedAt)}
            </time>
          )}
        </div>
      )}
      {mismatches.map(({ integration, entry }) => {
        if (!entry) return null;
        const hours =
          entry.platform === 'yandex_business' && entry.driftFields.includes('schedule');
        const differences = hours
          ? scheduleDifferences(entry.remoteValues?.schedule, entry.storedValues?.schedule)
          : [];
        const known = entry.driftFields.filter((field) => KNOWN_FIELDS.has(field));
        return (
          <article
            key={integration.id}
            className="space-y-4 rounded-block bg-card p-block md:p-block-desktop"
          >
            <h3 className="flex items-start gap-2 font-semibold">
              <PlatformIcon platform={integration.platform} className="mt-0.5 h-5 w-5 shrink-0" />
              {hours ? t('hoursDifferent') : t('dataDifferent', { platform: name(integration) })}
            </h3>
            {hours
              ? differences.map((difference) => (
                  <div key={difference.day}>
                    <p className="mb-2 text-meta text-ink-soft">{days(difference.day)}</p>
                    <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-4 gap-y-2 text-[15px] leading-[22px]">
                      <dt className="text-ink-soft">{t('onMaps')}</dt>
                      <dd>
                        <span className="inline-block rounded bg-diff-old px-1 font-semibold">
                          {openingHours(difference.remote, t('dayOff'))}
                        </span>
                      </dd>
                      <dt className="text-ink-soft">{t('yours')}</dt>
                      <dd>
                        <span className="inline-block rounded bg-diff-new px-1 font-semibold">
                          {openingHours(difference.stored, t('dayOff'))}
                        </span>
                      </dd>
                    </dl>
                  </div>
                ))
              : known.map((field) => (
                  <div key={field}>
                    <p className="mb-2 text-meta text-ink-soft">{fields(field)}</p>
                    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 break-words text-[15px] leading-[22px]">
                      <dt className="text-ink-soft">{name(integration)}</dt>
                      <dd>
                        <span className="whitespace-pre-wrap rounded bg-diff-old px-1">
                          {entry.remoteValues?.[field] || t('empty')}
                        </span>
                      </dd>
                      <dt className="text-ink-soft">{t('yours')}</dt>
                      <dd>
                        <span className="whitespace-pre-wrap rounded bg-diff-new px-1">
                          {entry.storedValues?.[field] || t('empty')}
                        </span>
                      </dd>
                    </dl>
                  </div>
                ))}
            <p className="text-[15px] leading-[22px] text-ink-soft">
              {t(hours ? 'hoursExplanation' : 'dataExplanation')}
            </p>
            {canEdit && (
              <div className="flex flex-col gap-2">
                {hours ? (
                  <Button
                    disabled={restore.isPending || adopt.isPending}
                    onClick={() => restore.mutate()}
                  >
                    {t('restoreHours')}
                  </Button>
                ) : (
                  <AlertDialog>
                    <AlertDialogTrigger asChild>
                      <Button variant="secondary" disabled={verify.isPending}>
                        {t('restoreData')}
                      </Button>
                    </AlertDialogTrigger>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>{t('confirmTitle')}</AlertDialogTitle>
                        <AlertDialogDescription>{t('confirmBody')}</AlertDialogDescription>
                        <ul className="space-y-1 text-[15px] leading-[22px]">
                          {integrations.map((row) => (
                            <li key={row.id}>
                              {name(row)}
                              {getIntegrationDisplay(row, name(row)).name !== name(row)
                                ? ` · ${getIntegrationDisplay(row, name(row)).name}`
                                : ''}
                            </li>
                          ))}
                        </ul>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>{t('cancel')}</AlertDialogCancel>
                        <AlertDialogAction onClick={genericRepair}>
                          {t('confirm')}
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                )}
                {hours && hasCompleteYandexHours(entry.remoteValues?.schedule) && (
                  <Button
                    variant="secondary"
                    disabled={adopt.isPending || restore.isPending}
                    onClick={() => adopt.mutate(entry)}
                  >
                    {t('adoptHours')}
                  </Button>
                )}
              </div>
            )}
          </article>
        );
      })}
      {showAlertSettings && <DriftAlertSettings businessId={businessId} />}
    </div>
  );
}
