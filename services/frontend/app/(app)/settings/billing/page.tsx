'use client';

import { useMemo, type ReactNode } from 'react';
import { useTranslations } from 'next-intl';

import { RequirePermission } from '@/components/permission/RequirePermission';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
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
  const pct = max > 0 ? Math.min(100, Math.max(0, Math.round((value / max) * 100))) : 0;
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={max}
      className="h-2 w-full overflow-hidden rounded-full bg-paper-sunken"
    >
      <div className="h-full rounded-full bg-ochre" style={{ width: `${pct}%` }} />
    </div>
  );
}

function SummaryCards({ summary }: { summary: BillingSummary }) {
  const t = useTranslations('settings.billing');
  const { plan, credits, usage_this_month, daily_spend } = summary;

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0">
          <CardTitle>{t('plan.title')}</CardTitle>
          <Badge tone="accent">{plan.code}</Badge>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-2 gap-4">
            <Stat label={t('plan.name')} value={plan.name} />
            <Stat label={t('plan.monthlyCredits')} value={plan.monthly_credits} />
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('credits.title')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <dl className="grid grid-cols-2 gap-4">
            <Stat label={t('credits.remaining')} value={credits.remaining} />
            <Stat label={t('credits.used')} value={credits.used} />
            <Stat label={t('credits.granted')} value={credits.granted} />
            <Stat label={t('credits.overage')} value={credits.overage} />
          </dl>
          <Gauge value={credits.used} max={credits.granted} label={t('credits.gaugeLabel')} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('usage.title')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-3 gap-4">
            <Stat label={t('usage.actions')} value={usage_this_month.actions} />
            <Stat label={t('usage.spend')} value={formatUsd(usage_this_month.spend_usd)} />
            <Stat label={t('usage.images')} value={usage_this_month.images} />
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('daily.title')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <dl className="grid grid-cols-2 gap-4">
            <Stat label={t('daily.today')} value={formatUsd(daily_spend.today_usd)} />
            <Stat label={t('daily.cap')} value={formatUsd(daily_spend.cap_usd)} />
          </dl>
          <Gauge
            value={daily_spend.today_usd}
            max={daily_spend.cap_usd}
            label={t('daily.gaugeLabel')}
          />
        </CardContent>
      </Card>
    </div>
  );
}

function LoadingCards() {
  return (
    <div aria-busy="true" className="grid grid-cols-1 gap-4 lg:grid-cols-2">
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
    </div>
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
      <section className="space-y-6 px-4 pb-10 pt-6 sm:px-12 sm:pb-16">
        <header className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight text-ink">{t('title')}</h1>
          <p className="text-ink-soft">{t('subtitle')}</p>
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
