'use client';

import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { IntegrationsSyncPanel } from '@/components/integrations/IntegrationsSyncPanel';
import { usePermission } from '@/lib/hooks/usePermission';
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
  const permission = usePermission('integrations.read');
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
    enabled: permission.allowed,
  });
  const active = integrations?.filter((i) => i.status === 'active') ?? [];
  if (!permission.allowed && !permission.isLoading) return null;
  return (
    <section id="comparison" className="scroll-mt-4">
      {isError ? (
        <ListLoadError onRetry={() => void refetch()} />
      ) : isLoading ? (
        <p className="text-meta text-ink-soft">{t('loading')}</p>
      ) : (
        <>
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
