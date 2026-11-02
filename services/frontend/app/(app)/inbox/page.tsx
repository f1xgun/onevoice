'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useLocale, useTranslations } from 'next-intl';
import { CheckCircle2 } from 'lucide-react';
import { useDecisionQueue } from '@/hooks/useDecisionQueue';
import type { DecisionItem } from '@/hooks/useDecisionQueue';
import { useBusinessStore } from '@/lib/stores/business';
import { useOnboardingProgress } from '@/hooks/useOnboardingProgress';
import { GettingStartedChecklist } from '@/components/onboarding/GettingStartedChecklist';
import { ActionButton } from '@/components/design-system/ActionButton';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { cn } from '@/lib/utils';
import { ReviewDecision } from './ReviewDecision';

export default function InboxPage() {
  const t = useTranslations('inbox');
  const tDrift = useTranslations('integrations.sync.fields');
  const locale = useLocale();
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const queue = useDecisionQueue();
  const progress = useOnboardingProgress();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const items = queue.data?.items ?? [];
  const selected = items.find((item) => item.id === selectedId) ?? items[0];
  function itemTitle(item: DecisionItem) {
    return item.review?.authorName || t(item.kind);
  }
  function itemSummary(item: DecisionItem) {
    if (item.review) return item.review.text;
    if (item.kind === 'approval_batch' && item.callCount)
      return t('approvalSummary', { count: item.callCount });
    if (item.kind === 'drift' && item.drift?.driftFields.length)
      return t('driftSummary', {
        count: item.drift.driftFields.length,
        fields: item.drift.driftFields
          .map((field) => (tDrift.has(field) ? tDrift(field) : field))
          .join(', '),
      });
    return null;
  }
  function target(item: DecisionItem) {
    if (item.kind === 'approval_batch') return `/chat/${item.conversationId}`;
    if (item.kind === 'connection') return `/integrations?reconnect=${item.platform}`;
    return '/business#comparison';
  }
  return (
    <div className="space-y-title-gap px-gutter pb-10 pt-5 xl:px-gutter-desktop">
      <header>
        <h1 className="flex items-center gap-3 text-page-title">
          {t('title')}
          {queue.data && queue.data.total > 0 && (
            <span className="rounded-full bg-count px-2 py-1 text-meta text-count-foreground">
              {queue.data.total}
            </span>
          )}
        </h1>
        <p className="mt-1 text-meta text-ink-soft">
          {new Intl.DateTimeFormat(locale, {
            weekday: 'long',
            day: 'numeric',
            month: 'long',
          }).format(new Date())}
        </p>
      </header>
      {!businessId ? (
        <p>{t('chooseBusiness')}</p>
      ) : queue.isPending ? (
        <p role="status">{t('loading')}</p>
      ) : queue.isError ? (
        <div role="alert" className="space-y-3 rounded-block bg-block p-block">
          <p>{t('loadError')}</p>
          <ActionButton variant="secondary" onClick={() => void queue.refetch()}>
            {t('retry')}
          </ActionButton>
        </div>
      ) : (
        <div className={cn('grid gap-group', selected && 'xl:grid-cols-[372px_minmax(0,1fr)]')}>
          {selected ? (
            <section className="min-w-0 xl:col-start-2 xl:row-start-1">
              {selected.review ? (
                <ReviewDecision
                  key={`${businessId}:${selected.id}`}
                  review={selected.review}
                  businessId={businessId}
                />
              ) : (
                <article className="space-y-4 rounded-block bg-block p-block md:p-block-desktop">
                  <h2 className="text-block-title">{itemTitle(selected)}</h2>
                  <p className="text-reading">
                    {t(`${selected.kind}Body`, { count: selected.callCount ?? 0 })}
                  </p>
                  {selected.drift && (
                    <p className="text-meta text-ink-soft">
                      {selected.drift.driftFields
                        .map((field) => (tDrift.has(field) ? tDrift(field) : field))
                        .join(', ')}
                    </p>
                  )}
                  <ActionButton asChild>
                    <Link href={target(selected)}>{t(`${selected.kind}Action`)}</Link>
                  </ActionButton>
                </article>
              )}
            </section>
          ) : (
            <section className="max-w-[760px] space-y-group">
              {progress.loaded && !progress.allDone ? (
                <GettingStartedChecklist dismissible={false} />
              ) : (
                <article className="space-y-2 rounded-block bg-block p-block md:p-block-desktop">
                  <h2 className="text-block-title">{t('emptyTitle')}</h2>
                  <p className="text-reading text-ink-soft">{t('emptyBody')}</p>
                </article>
              )}
            </section>
          )}
          <aside
            className={cn(
              'min-w-0 space-y-group',
              selected ? 'xl:col-start-1 xl:row-start-1' : 'max-w-[760px]'
            )}
          >
            {items.length > 0 && (
              <section className={items.length === 1 ? 'hidden xl:block' : undefined}>
                <h2 className="mb-block-gap text-group-title xl:hidden">{t('more')}</h2>
                <ul className="rounded-block bg-block p-1 md:p-2">
                  {items.map((item) => (
                    <li key={item.id} className={item.id === selected?.id ? 'hidden xl:block' : ''}>
                      <button
                        type="button"
                        onClick={() => setSelectedId(item.id)}
                        aria-pressed={item.id === selected?.id}
                        className={cn(
                          'flex min-h-tap w-full items-start gap-3 rounded-inner p-3 text-left hover:bg-inset',
                          item.id === selected?.id && 'bg-brand-tone'
                        )}
                      >
                        <PlatformIcon platform={item.platform ?? 'onevoice'} />
                        <span className="min-w-0">
                          <span className="block font-semibold">{itemTitle(item)}</span>
                          {item.review && (
                            <span className="block text-meta text-ink-soft">{t(item.kind)}</span>
                          )}
                          {itemSummary(item) && (
                            <span className="line-clamp-2 text-reading">{itemSummary(item)}</span>
                          )}
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            {queue.data && queue.data.total > items.length && (
              <Link
                href="/reviews"
                className="inline-flex min-h-tap items-center text-brand underline"
              >
                {t('allReviews')}
              </Link>
            )}
            {(queue.data?.automatic.length ?? 0) > 0 && (
              <section>
                <h2 className="text-group-title">{t('automatic')}</h2>
                <p className="mb-block-gap mt-1 text-meta text-ink-soft">{t('automaticHint')}</p>
                <ul className="space-y-3">
                  {queue.data?.automatic.map((receipt) => (
                    <li
                      key={receipt.id}
                      className="flex items-start gap-2 text-[15px] leading-[22px]"
                    >
                      <CheckCircle2
                        aria-hidden
                        className="mt-1 h-head-sm w-head-sm shrink-0 text-ok"
                      />
                      <span className="flex-1">{t('autoReview')}</span>
                      <time className="text-meta text-ink-soft" dateTime={receipt.createdAt}>
                        {new Intl.DateTimeFormat(locale, {
                          hour: '2-digit',
                          minute: '2-digit',
                        }).format(new Date(receipt.createdAt))}
                      </time>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            <ActionButton asChild variant="secondary" className="w-full justify-between !bg-block">
              <Link href="/chat">
                {t('instruct')}
                <span aria-hidden>→</span>
              </Link>
            </ActionButton>
          </aside>
        </div>
      )}
    </div>
  );
}
