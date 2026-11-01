'use client';

import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useIntegrationsDrift } from '@/lib/hooks/useIntegrationsDrift';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { IntegrationsSyncPanel } from '@/components/integrations/IntegrationsSyncPanel';
import { usePlatformMeta, type PlatformId } from '@/lib/platforms';
import { ListLoadError } from '@/components/lists/ListLoadError';

interface Integration {
  id: string;
  platform: string;
  externalId: string;
  status: string;
}
interface BusinessComparisonProps {
  businessId: string;
}

export function BusinessComparison({ businessId }: BusinessComparisonProps) {
  const t = useTranslations('business.card');
  const platformMeta = usePlatformMeta();
  const driftFields = useTranslations('integrations.sync.fields');
  const { data: drift } = useIntegrationsDrift(businessId);
  const {
    data: integrations,
    isError,
    isLoading,
    refetch,
  } = useQuery<Integration[]>({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(businessId),
    queryFn: () =>
      bizApi(businessId)
        .get<Integration[]>(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then((r) => {
          if (!Array.isArray(r.data)) throw new Error('Invalid integration list response');
          return r.data;
        }),
  });
  const active = integrations?.filter((i) => i.status === 'active') ?? [];
  return (
    <section id="comparison" className="scroll-mt-4">
      {isError ? (
        <ListLoadError onRetry={() => void refetch()} />
      ) : isLoading ? (
        <p className="text-meta text-ink-soft">{t('loading')}</p>
      ) : (
        <>
          {(drift ?? [])
            .filter(
              (entry) =>
                !entry.checkFailed &&
                entry.lastCheckedAt &&
                active.some(
                  (i) => i.platform === entry.platform && i.externalId === entry.externalId
                )
            )
            .map((entry) => (
              <div key={`${entry.platform}:${entry.externalId}`} className="space-y-3">
                {entry.driftFields.map(
                  (field) =>
                    entry.remoteValues?.[field] !== undefined &&
                    entry.storedValues?.[field] !== undefined && (
                      <div key={field} className="rounded-block bg-card p-block md:p-block-desktop">
                        <h3 className="font-semibold">
                          {t('difference', { field: driftFields(field) })}
                        </h3>
                        <dl className="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-2 break-words text-sm">
                          <dt className="text-ink-soft">
                            {platformMeta[entry.platform as PlatformId]?.fullLabel ??
                              entry.platform}
                          </dt>
                          <dd className="whitespace-pre-wrap rounded bg-diff-old px-1">
                            {entry.remoteValues[field] || t('empty')}
                          </dd>
                          <dt className="text-ink-soft">{t('stored')}</dt>
                          <dd className="whitespace-pre-wrap rounded bg-diff-new px-1">
                            {entry.storedValues[field] || t('empty')}
                          </dd>
                        </dl>
                      </div>
                    )
                )}
              </div>
            ))}
          <IntegrationsSyncPanel
            businessId={businessId}
            integrations={active}
            repairLabel={t('repair')}
            showAlertSettings={false}
          />
          {integrations?.length === 0 && (
            <p className="text-meta text-ink-soft">{t('noPlatforms')}</p>
          )}
        </>
      )}
    </section>
  );
}
