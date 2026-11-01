'use client';

import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';

import { useMemo, type ReactNode } from 'react';
import { useTranslations } from 'next-intl';

import { ProRequest } from '@/components/billing/ProRequest';
import { RequirePermission } from '@/components/permission/RequirePermission';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { useBusinessStore } from '@/lib/stores/business';
import type { BillingSummary } from '@/lib/api/billing';
import { useBillingSummary } from './_lib/useBillingSummary';

const BILLING_READ = 'billing.read';

function formatUsd(value: number): string {
  return `$${value.toFixed(2)}`;
}

function Stat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-sm text-ink-soft">{label}</dt>
      <dd className="text-lg font-semibold text-ink">{value}</dd>
    </div>
  );
}

function Gauge({ value, max, label }: { value: number; max: number; label: string }) {
  return (
    <progress
      role="progressbar"
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={max}
      value={value}
      max={max}
      className="h-2 w-full overflow-hidden rounded-full bg-inset [&::-moz-progress-bar]:rounded-full [&::-moz-progress-bar]:bg-brand [&::-webkit-progress-bar]:bg-inset [&::-webkit-progress-value]:rounded-full [&::-webkit-progress-value]:bg-brand"
    />
  );
}

function SummaryCards({ summary }: { summary: BillingSummary }) {
  const t = useTranslations('settings.billing');
  const tV2 = useTranslations('settings.v2');
  const { plan, credits, usage_this_month, daily_spend } = summary;
  const isFree = plan.code.toLowerCase() === 'free';
  const units = usage_this_month.units;
  return (
    <div className="space-y-group">
      <section className="space-y-3 rounded-block bg-block p-block md:p-block-desktop">
        <h2 className="text-group-title">{plan.name}</h2>
        <p className="text-reading">
          {credits.granted < 0
            ? t('unlimited')
            : tV2('remainingUnits', { remaining: credits.remaining, total: credits.granted })}
        </p>
        {credits.granted > 0 && (
          <Gauge value={credits.remaining} max={credits.granted} label={tV2('remainingGauge')} />
        )}
        {credits.remaining === 0 && credits.granted >= 0 && (
          <p className="text-[15px] leading-[22px] text-danger">{tV2('unitsExhausted')}</p>
        )}
      </section>
      <section>
        <h2 className="mb-block-gap text-group-title">{tV2('monthlyUsage')}</h2>
        <div className="space-y-3 rounded-block bg-block p-block md:p-block-desktop">
          <dl className="space-y-2">
            {(units
              ? [
                  [tV2('modelUnits'), units.text],
                  [tV2('imageUnits'), units.images],
                  ...(units.other > 0 ? [[tV2('otherUnits'), units.other]] : []),
                  [tV2('totalUnits'), units.total],
                ]
              : [
                  [t('usage.actions'), usage_this_month.actions],
                  [t('usage.images'), usage_this_month.images],
                ]
            ).map(([label, value]) => (
              <div key={label} className="flex justify-between gap-3 text-reading">
                <dt>{label}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
          <p className="text-meta text-ink-soft">{tV2('unitMeaning')}</p>
        </div>
      </section>
      {!isFree && (
        <details className="rounded-block bg-block p-block md:p-block-desktop">
          <summary className="min-h-tap cursor-pointer">{t('daily.title')}</summary>
          <dl className="mt-3 grid grid-cols-2 gap-4">
            <Stat label={t('usage.spend')} value={formatUsd(usage_this_month.spend_usd)} />
            <Stat label={t('daily.today')} value={formatUsd(daily_spend.today_usd)} />
            <Stat
              label={t('daily.cap')}
              value={daily_spend.cap_usd < 0 ? t('unlimited') : formatUsd(daily_spend.cap_usd)}
            />
            <Stat
              label={t('plan.monthlyCredits')}
              value={plan.monthly_credits < 0 ? t('unlimited') : plan.monthly_credits}
            />
          </dl>
          {daily_spend.cap_usd > 0 && (
            <Gauge
              value={daily_spend.today_usd}
              max={daily_spend.cap_usd}
              label={t('daily.gaugeLabel')}
            />
          )}
        </details>
      )}
      {isFree && (
        <section>
          <h2 className="text-group-title">{t('pro.title')}</h2>
          <p className="mb-block-gap mt-1 text-[15px] leading-[22px] text-ink-soft">
            {t('pro.price')}
          </p>
          <div className="space-y-4 rounded-block bg-block p-block md:p-block-desktop">
            <p className="text-reading">{tV2('proBenefits')}</p>
            <ProRequest source="billing" />
          </div>
        </section>
      )}
    </div>
  );
}

function LoadingCards() {
  return (
    <LoadingPlaceholder aria-busy="true" className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      {[0, 1, 2, 3].map((i) => (
        <Card key={i}>
          <CardHeader>
            <Skeleton className="h-5 w-40" />
          </CardHeader>
          <CardContent>
            <Skeleton className="h-16 w-full" />
          </CardContent>
        </Card>
      ))}
    </LoadingPlaceholder>
  );
}

export default function BillingPage() {
  const t = useTranslations('settings.billing');
  const businessID = useBusinessStore((s) => s.activeBusinessId);
  const { data, isLoading, isSuccess, error } = useBillingSummary(businessID);

  const errorMessage = useMemo(() => {
    if (!error) return null;
    if (error.code === 'forbidden') return t('errors.forbidden');
    return t('errors.loadFailed');
  }, [error, t]);

  return (
    <RequirePermission
      perm={BILLING_READ}
      fallback={
        <div className="px-4 pb-10 pt-6 sm:px-12 sm:pb-16">
          <h1 className="text-2xl font-semibold tracking-tight text-ink">{t('title')}</h1>
          <p className="mt-4 rounded-md border border-line bg-paper-sunken p-3 text-ink">
            {t('errors.forbidden')}
          </p>
        </div>
      }
    >
      <section className="mx-auto max-w-[816px] space-y-title-gap px-gutter pb-10 pt-5 md:px-gutter-desktop">
        <header className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight text-ink">{t('title')}</h1>
        </header>

        {errorMessage ? (
          <div role="alert" className="rounded-md border border-destructive p-3 text-destructive">
            {errorMessage}
          </div>
        ) : isLoading ? (
          <LoadingCards />
        ) : isSuccess && data ? (
          <SummaryCards summary={data} />
        ) : null}
      </section>
    </RequirePermission>
  );
}
