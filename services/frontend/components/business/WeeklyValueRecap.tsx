'use client';

import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { z } from 'zod';
import { useAuthStore } from '@/lib/auth';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { trackEvent } from '@/lib/telemetry';
import { usePermission } from '@/lib/hooks/usePermission';

const countSchema = z.number().finite().int().nonnegative();
export const weeklyRecapSchema = z
  .object({
    weekStart: z.string().datetime({ offset: true }),
    weekEnd: z.string().datetime({ offset: true }),
    publishedPosts: countSchema,
    dispatchedReviewReplies: countSchema,
    completedSyncs: countSchema,
  })
  .refine((value) => Date.parse(value.weekEnd) > Date.parse(value.weekStart), 'invalid week window')
  .refine(
    (value) => value.publishedPosts + value.dispatchedReviewReplies + value.completedSyncs >= 2,
    'suppressed recap'
  );
type WeeklyRecap = z.infer<typeof weeklyRecapSchema>;

const shownRecapKeys = new Set<string>();

export function weeklyRecapDismissKey(userId: string, businessId: string, weekStart: string) {
  return `onevoice:weekly-recap:${userId}:${businessId}:${weekStart}`;
}

export function WeeklyValueRecap() {
  const t = useTranslations('weeklyRecap');
  const locale = useLocale();
  const userId = useAuthStore((s) => s.user?.id);
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const readPermission = usePermission('content.read');
  const [dismissal, setDismissal] = useState<{ key: string; dismissed: boolean } | null>(null);
  const { data } = useQuery<WeeklyRecap | undefined>({
    queryKey: QUERY_KEYS.BUSINESS_RECAP(businessId),
    queryFn: () =>
      bizApi(businessId!)
        .get<WeeklyRecap>(BIZ_API_PATHS.RECAP.LATEST)
        .then((response) =>
          response.status === 204 ? undefined : weeklyRecapSchema.parse(response.data)
        ),
    enabled: !!businessId && !!userId && readPermission.allowed,
    retry: false,
  });

  const storageKey =
    data && userId && businessId
      ? weeklyRecapDismissKey(userId, businessId, data.weekStart)
      : undefined;

  useEffect(() => {
    if (!storageKey || !businessId) {
      setDismissal(null);
      return;
    }
    let isDismissed = false;
    try {
      isDismissed = window.localStorage.getItem(storageKey) === '1';
    } catch {}
    setDismissal({ key: storageKey, dismissed: isDismissed });
    if (!isDismissed && !shownRecapKeys.has(storageKey)) {
      shownRecapKeys.add(storageKey);
      trackEvent('value_recap', 'shown', { page: '/business', businessId });
    }
  }, [businessId, storageKey]);

  if (!data || !storageKey || dismissal?.key !== storageKey || dismissal.dismissed) return null;

  const categories = [
    data.publishedPosts > 0 && t('posts', { count: data.publishedPosts }),
    data.dispatchedReviewReplies > 0 && t('replies', { count: data.dispatchedReviewReplies }),
    data.completedSyncs > 0 && t('syncs', { count: data.completedSyncs }),
  ].filter(Boolean) as string[];
  const formatter = new Intl.DateTimeFormat(locale, {
    day: 'numeric',
    month: 'short',
    timeZone: 'UTC',
  });

  function dismiss() {
    try {
      window.localStorage.setItem(storageKey!, '1');
    } catch {}
    setDismissal({ key: storageKey!, dismissed: true });
    trackEvent('value_recap', 'dismissed', { page: '/business', businessId });
  }

  return (
    <section
      aria-label={t('title')}
      className="mx-4 mt-4 rounded-lg border border-brand-soft bg-brand-soft px-4 py-4 text-ink sm:mx-12 sm:flex sm:items-start sm:justify-between sm:gap-6"
    >
      <div className="min-w-0">
        <p className="text-sm font-semibold">{t('title')}</p>
        <p className="mt-1 text-[13px] text-ink-mid">
          {t('period', {
            from: formatter.format(new Date(data.weekStart)),
            to: formatter.format(new Date(new Date(data.weekEnd).getTime() - 1)),
          })}
        </p>
        <ul className="mt-3 flex flex-wrap gap-x-5 gap-y-2 text-sm">
          {categories.map((category) => (
            <li key={category}>{category}</li>
          ))}
        </ul>
        <p className="mt-3 text-[12px] text-ink-mid">{t('footnote')}</p>
      </div>
      <button
        type="button"
        onClick={dismiss}
        aria-label={t('dismiss')}
        className="hover:bg-paper/60 mt-3 inline-flex min-h-11 min-w-11 items-center justify-center rounded-md text-ink-mid hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink sm:mt-0"
      >
        <X className="size-4" aria-hidden />
      </button>
    </section>
  );
}
