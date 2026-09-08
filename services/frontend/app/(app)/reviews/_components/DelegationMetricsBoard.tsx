'use client';

import { useLocale, useTranslations } from 'next-intl';
import { z } from 'zod';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Skeleton } from '@/components/ui/skeleton';
import { localeToIntlTag, type Locale } from '@/lib/i18n/locales';

const count = z.number().int().nonnegative();
const RECENT_WEEK_COUNT = 8;
const week = z.object({
  weekStart: z.string().datetime(),
  replied: count,
  acceptedUnedited: count,
  edited: count,
  unknown: count,
});
const schema = z.object({
  from: z.string().datetime(),
  to: z.string().datetime(),
  replied: count,
  acceptedUnedited: count,
  edited: count,
  unknown: count,
  measurable: count,
  weeks: z.array(week).max(RECENT_WEEK_COUNT),
});
export type DelegationMetrics = z.infer<typeof schema>;
export const parseDelegationMetrics = (data: unknown): DelegationMetrics => schema.parse(data);

export function DelegationMetricsBoard({
  data,
  isLoading,
  isError,
  onRetry,
}: {
  data?: DelegationMetrics;
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
}) {
  const t = useTranslations('reviews.delegationMetrics');
  const locale = useLocale() as Locale;
  if (isLoading)
    return <Skeleton data-testid="delegation-metrics-loading" className="mb-6 h-52 rounded-lg" />;
  if (isError)
    return (
      <section className="mb-6 rounded-lg border border-line bg-paper-raised p-5" role="alert">
        <p>{t('error')}</p>
        <Button className="mt-3" variant="ghost" onClick={onRetry}>
          {t('retry')}
        </Button>
      </section>
    );
  if (!data) return null;
  const acceptedShare =
    data.measurable > 0 ? Math.round((data.acceptedUnedited / data.measurable) * 100) : null;
  return (
    <section
      aria-labelledby="delegation-title"
      className="mb-6 rounded-lg border border-line bg-paper-raised p-5"
    >
      <h2 id="delegation-title" className="text-lg font-semibold text-ink">
        {t('title')}
      </h2>
      <p className="mt-1 text-sm text-ink-soft">{t('description')}</p>
      <div className="mt-4 grid gap-3 sm:grid-cols-3">
        <Metric label={t('accepted')} value={data.acceptedUnedited} />
        <Metric label={t('edited')} value={data.edited} />
        <Metric label={t('unknown')} value={data.unknown} />
      </div>
      <p className="mt-3 text-xs text-ink-soft">
        {acceptedShare === null
          ? t('noKnown')
          : t('knownShare', { percent: acceptedShare, count: data.measurable })}
      </p>
      <div className="mt-5 space-y-2" aria-label={t('weekly')}>
        {data.weeks.map((w) => (
          <div
            key={w.weekStart}
            className="grid grid-cols-[5rem_1fr_auto] items-center gap-3 text-xs"
          >
            <span>
              {new Intl.DateTimeFormat(localeToIntlTag(locale), {
                day: 'numeric',
                month: 'short',
                timeZone: 'UTC',
              }).format(new Date(w.weekStart))}
            </span>
            <div className="flex h-2 overflow-hidden rounded bg-paper-sunken">
              <span
                className="bg-success"
                style={{ width: `${w.replied ? (w.acceptedUnedited / w.replied) * 100 : 0}%` }}
              />
              <span
                className="bg-ochre"
                style={{ width: `${w.replied ? (w.edited / w.replied) * 100 : 0}%` }}
              />
            </div>
            <span className="tabular-nums text-ink-soft">
              {w.acceptedUnedited}/{w.edited}/{w.unknown}
            </span>
          </div>
        ))}
      </div>
      <p className="mt-3 text-xs text-ink-soft">{t('legend')}</p>
    </section>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-md bg-paper-sunken p-3">
      <div className="text-xs text-ink-soft">{label}</div>
      <div className="mt-1 text-2xl font-medium text-ink">{value}</div>
    </div>
  );
}
