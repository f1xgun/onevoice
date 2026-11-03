'use client';

import Image from 'next/image';
import { useQuery } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { usePermission } from '@/lib/hooks/usePermission';
import { useIntegrationsDrift } from '@/lib/hooks/useIntegrationsDrift';
import { bizApi } from '@/lib/api/business-api';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { shortBusinessAddress } from '@/lib/business-display';
import { effectiveHours, openingHours } from '@/lib/business-schedule';
import { usePlatformMeta, type PlatformId } from '@/lib/platforms';
import type { Business, ScheduleDay, SpecialDate } from '@/types/business';

interface Props {
  business: Business;
  businessId: string;
  schedule: ScheduleDay[];
  specialDates: SpecialDate[];
}
interface Integration {
  id: string;
  platform: string;
  externalId: string;
  status: string;
  metadata?: Record<string, unknown>;
}
const MAX_RATING = 5;
const CHECKED_FIELDS: Record<string, string[]> = {
  yandex_business: ['schedule'],
  vk: ['title', 'description', 'website'],
  telegram: ['title', 'description'],
};

export function BusinessPresence({ business, businessId, schedule, specialDates }: Props) {
  const t = useTranslations('business.card');
  const fields = useTranslations('integrations.sync.fields');
  const locale = useLocale();
  const platforms = usePlatformMeta();
  const permission = usePermission('integrations.read');
  const query = useQuery({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(businessId),
    queryFn: async () => {
      const { data } = await bizApi(businessId).get<unknown>('/integrations');
      return Array.isArray(data) ? (data as Integration[]) : [];
    },
    enabled: permission.allowed,
  });
  const drift = useIntegrationsDrift(permission.allowed ? businessId : null);
  const integrations = permission.allowed
    ? (query.data?.filter((row) => row.status === 'active') ?? [])
    : [];
  const maps = integrations.find((row) => row.platform === 'yandex_business')?.metadata;
  const rating = Number(maps?.rating),
    count = Number(maps?.ratings_count);
  const today = effectiveHours(schedule, specialDates);
  const todayHours = today && openingHours(today, t('dayOff'));
  const compared = integrations
    .map((integration) => ({
      integration,
      entry: drift.data?.find(
        (row) => row.platform === integration.platform && row.externalId === integration.externalId
      ),
    }))
    .filter(({ entry }) => entry?.lastCheckedAt && !entry.checkFailed);
  return (
    <aside className="hidden min-w-0 xl:block">
      <h2 className="mb-2.5 text-group-title">{t('preview')}</h2>
      <div className="space-y-3 break-words rounded-block bg-card p-block-desktop">
        {business.logoUrl && (
          <Image
            src={business.logoUrl}
            alt=""
            width={280}
            height={160}
            unoptimized
            className="h-40 w-full rounded-inner object-cover"
          />
        )}
        <h3 className="text-lg font-semibold">{business.name}</h3>
        {Number.isFinite(rating) && rating > 0 && rating <= MAX_RATING && (
          <p className="flex flex-wrap items-center gap-2 text-[15px] leading-[22px]">
            <strong>
              {new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(rating)}
            </strong>
            <span aria-hidden>
              {Array.from({ length: MAX_RATING }, (_, index) => (
                <span
                  key={index}
                  className={index < Math.round(rating) ? 'text-star' : 'text-line'}
                >
                  {'★'.repeat(1)}
                </span>
              ))}
            </span>
            {Number.isFinite(count) && count > 0 && (
              <span className="text-meta text-ink-soft">{t('ratingCount', { count })}</span>
            )}
          </p>
        )}
        <p className="text-[15px] leading-[22px] text-ink-soft">
          {[business.category, business.address && shortBusinessAddress(business.address)]
            .filter(Boolean)
            .join(' · ')}
        </p>
        {todayHours && (
          <p className="text-[15px] leading-[22px] text-ink-soft">
            {today?.closed ? t('todayOff') : t('todayHours', { hours: todayHours })}
          </p>
        )}
        {business.phone && <p className="text-[15px] leading-[22px]">{business.phone}</p>}
      </div>
      {compared.length > 0 && (
        <section className="mt-5">
          <h2 className="mb-2.5 text-group-title">{t('whereMatches')}</h2>
          <ul className="space-y-2">
            {compared.map(({ integration, entry }) => {
              if (!entry) return null;
              const checked = CHECKED_FIELDS[integration.platform] ?? [];
              const changed = entry.driftFields.filter((field) => checked.includes(field));
              if (entry.driftDetected && changed.length === 0) return null;
              const fieldNames = (entry.driftDetected ? changed : checked).map((field) =>
                fields(field)
              );
              if (fieldNames.length === 0) return null;
              return (
                <li
                  key={integration.id}
                  className="flex items-start gap-2 text-[15px] leading-[22px]"
                >
                  <PlatformIcon
                    platform={integration.platform}
                    className="mt-0.5 h-4 w-4 shrink-0"
                  />
                  <span className="min-w-0 break-words">
                    {t(entry.driftDetected ? 'platformDifferent' : 'platformMatches', {
                      platform:
                        platforms[integration.platform as PlatformId]?.fullLabel || t('platform'),
                      fields: new Intl.ListFormat(locale, { type: 'conjunction' }).format(
                        fieldNames
                      ),
                    })}
                  </span>
                </li>
              );
            })}
          </ul>
        </section>
      )}
    </aside>
  );
}
