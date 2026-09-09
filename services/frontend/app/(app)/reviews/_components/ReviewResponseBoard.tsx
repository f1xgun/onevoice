'use client';

import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';

import { useTranslations } from 'next-intl';
import { z } from 'zod';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Skeleton } from '@/components/ui/skeleton';
import { MonoLabel } from '@/components/ui/mono-label';
import { cn } from '@/lib/utils';

const countSchema = z.number().int().nonnegative();
const hoursSchema = z.number().nonnegative();
const reviewSLASchema = z.object({
  total: countSchema,
  unanswered: countSchema,
  answered: countSchema,
  buckets: z.object({
    lt24h: countSchema,
    h24to72: countSchema,
    gt72h: countSchema,
  }),
  targetHours: z.number().int().positive(),
  medianResponseHours: hoursSchema,
  averageResponseHours: hoursSchema,
  measuredResponses: countSchema,
  percentAnsweredWithinTarget: z.number().min(0).max(1),
  oldestUnansweredHours: hoursSchema.nullable(),
  platforms: z.array(
    z.object({
      platform: z.string().min(1),
      medianResponseHours: hoursSchema,
      measuredResponses: countSchema,
    })
  ),
});

export type ReviewSLAResponse = z.infer<typeof reviewSLASchema>;

export function parseReviewSLAResponse(data: unknown): ReviewSLAResponse {
  return reviewSLASchema.parse(data);
}

interface ReviewResponseBoardProps {
  data?: ReviewSLAResponse;
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  platformLabel: (platform: string) => string;
}

function formatHours(hours: number): string {
  return Number.isInteger(hours) ? String(hours) : hours.toFixed(1);
}

export function ReviewResponseBoard({
  data,
  isLoading,
  isError,
  onRetry,
  platformLabel,
}: ReviewResponseBoardProps) {
  const t = useTranslations('reviews.responseBoard');

  return (
    <section aria-labelledby="review-response-board-title" className="mb-6 space-y-4">
      <div>
        <h2 id="review-response-board-title" className="text-lg font-semibold text-ink">
          {t('title')}
        </h2>
        <p className="mt-1 text-sm text-ink-soft">{t('description')}</p>
      </div>

      {isLoading ? (
        <LoadingPlaceholder
          data-testid="response-board-loading"
          className="grid gap-3 sm:grid-cols-4"
        >
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} className="h-24 rounded-md" />
          ))}
        </LoadingPlaceholder>
      ) : isError ? (
        <div
          role="alert"
          className="border-danger/30 bg-danger/5 flex flex-wrap items-center justify-between gap-3 rounded-md border px-4 py-3"
        >
          <p className="text-sm text-ink">{t('error')}</p>
          <Button type="button" variant="secondary" size="sm" onClick={onRetry}>
            {t('retry')}
          </Button>
        </div>
      ) : data ? (
        <div className="space-y-4" data-testid="response-board-content">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Metric
              label={t('bands.lt24h')}
              value={data.buckets.lt24h}
              tone={data.buckets.lt24h > 0 ? 'success' : undefined}
            />
            <Metric
              label={t('bands.h24to72')}
              value={data.buckets.h24to72}
              tone={data.buckets.h24to72 > 0 ? 'warning' : undefined}
            />
            <Metric
              label={t('bands.gt72h')}
              value={data.buckets.gt72h}
              tone={data.buckets.gt72h > 0 ? 'danger' : undefined}
            />
            <Metric
              label={t('oldestLabel')}
              value={
                data.oldestUnansweredHours == null
                  ? t('noUnanswered')
                  : t('hoursValue', { hours: formatHours(data.oldestUnansweredHours) })
              }
            />
          </div>

          <div className="rounded-md border border-line bg-paper-raised px-5 py-4">
            <MonoLabel>{t('medianLabel')}</MonoLabel>
            {data.measuredResponses === 0 ? (
              <p className="mt-2 text-sm text-ink-soft">{t('noMeasurements')}</p>
            ) : (
              <>
                <div className="mt-1.5 text-[26px] font-medium leading-none text-ink">
                  {t('hoursValue', { hours: formatHours(data.medianResponseHours) })}
                </div>
                <p className="mt-1.5 text-xs text-ink-soft">
                  {t('measuredHint', { count: data.measuredResponses })}
                </p>
              </>
            )}

            {data.platforms.length > 0 ? (
              <div className="mt-4 border-t border-line-soft pt-3">
                <MonoLabel>{t('platformsLabel')}</MonoLabel>
                <ul className="mt-2 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                  {data.platforms.map((platform) => (
                    <li
                      key={platform.platform}
                      className="flex items-center justify-between gap-3 text-sm"
                    >
                      <span className="text-ink-mid">{platformLabel(platform.platform)}</span>
                      <span className="font-medium text-ink">
                        {t('hoursValue', {
                          hours: formatHours(platform.medianResponseHours),
                        })}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
        </div>
      ) : null}
    </section>
  );
}

interface MetricProps {
  label: string;
  value: string | number;
  tone?: 'success' | 'warning' | 'danger';
}

const metricToneClass: Record<NonNullable<MetricProps['tone']>, string> = {
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
};

function Metric({ label, value, tone }: MetricProps) {
  return (
    <div className="rounded-md border border-line bg-paper-raised px-5 py-4">
      <MonoLabel>{label}</MonoLabel>
      <div
        className={cn(
          'mt-1.5 text-[26px] font-medium leading-none',
          tone ? metricToneClass[tone] : 'text-ink'
        )}
      >
        {value}
      </div>
    </div>
  );
}
