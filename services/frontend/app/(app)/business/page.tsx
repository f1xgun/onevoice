'use client';

import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';

import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { isAxiosError } from 'axios';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { ProfileForm } from '@/components/business/ProfileForm';
import { BusinessCard } from '@/components/business/BusinessCard';
import { Skeleton } from '@/components/ui/skeleton';
import type { Business } from '@/types/business';

function BusinessSkeleton() {
  return (
    <LoadingPlaceholder className="grid grid-cols-1 gap-8 xl:grid-cols-[minmax(0,1fr)_minmax(0,320px)]">
      <div className="space-y-6">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-48 w-full rounded-block" />
        ))}
      </div>
    </LoadingPlaceholder>
  );
}

export default function BusinessPage() {
  const t = useTranslations('business.card');
  const tBusiness = useTranslations('business');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const { data, isLoading, isError, error, refetch } = useQuery<Business>({
    queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Business>(BIZ_API_PATHS.BUSINESS.ROOT)
        .then((r) => r.data),
    enabled: !!activeBusinessId,
    retry: false,
  });
  const is404 = isError && isAxiosError(error) && error.response?.status === HTTP_STATUS.NOT_FOUND;
  return (
    <div className="px-gutter pb-10 pt-2 md:px-gutter-desktop xl:pt-group">
      <header className="mb-4 xl:mb-group">
        <h1 className="text-page-title">{is404 ? tBusiness('createTitle') : t('title')}</h1>
        <p className="mt-1 text-[15px] leading-[22px] text-ink-soft">{t('subtitle')}</p>
      </header>
      {isLoading ? (
        <BusinessSkeleton />
      ) : isError && !is404 ? (
        <ListLoadError onRetry={() => void refetch()} />
      ) : is404 ? (
        <ProfileForm />
      ) : data && activeBusinessId ? (
        <BusinessCard key={activeBusinessId} business={data} businessId={activeBusinessId} />
      ) : null}
    </div>
  );
}
