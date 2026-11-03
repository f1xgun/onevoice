'use client';

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
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
import { InstructionComposer } from './InstructionComposer';
import { usePermission } from '@/lib/hooks/usePermission';
import { useIntegrationsDrift } from '@/lib/hooks/useIntegrationsDrift';
import { bizApi } from '@/lib/api/business-api';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { businessMark } from '@/lib/business-display';
import { decisionDay, decisionTimestamp, firstScheduleDifference } from '@/lib/decision-display';
import { formatClockTime } from '@/lib/time-display';
import type { Business } from '@/types/business';
import { ReviewDecision } from './ReviewDecision';

const MAX_RATING = 5;

export default function InboxPage() {
  const t = useTranslations('inbox');
  const tDays = useTranslations('business.daysOfWeek');
  const tDrift = useTranslations('integrations.sync.fields');
  const locale = useLocale();
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const queue = useDecisionQueue();
  const progress = useOnboardingProgress();
  const integrationsPermission = usePermission('integrations.read');
  const drift = useIntegrationsDrift(integrationsPermission.allowed ? businessId : null);
  const business = useQuery({
    queryKey: QUERY_KEYS.BUSINESS_PROFILE(businessId),
    queryFn: async () => (await bizApi(businessId!).get<Business>('')).data,
    enabled: !!businessId,
  });
  const integrations = useQuery({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(businessId),
    queryFn: async () => {
      const { data } = await bizApi(businessId!).get<unknown>('/integrations');
      return Array.isArray(data)
        ? (data as { platform: string; metadata?: Record<string, unknown> }[])
        : [];
    },
    enabled: !!businessId && integrationsPermission.allowed,
  });
  const maps = integrationsPermission.allowed
    ? integrations.data?.find((integration) => integration.platform === 'yandex_business')?.metadata
    : undefined;
  const rating = Number(maps?.rating),
    ratingCount = Number(maps?.ratings_count);
  function place(platform?: string) {
    return t(t.has(`place.${platform}`) ? `place.${platform}` : 'place.other');
  }
  function sourceLabel(item: DecisionItem) {
    if (!item.review) return '';
    if (item.review.rating > 0) return t('reviewAt', { platform: place(item.review.platform) });
    const title = item.review.platformMeta?.post_title;
    return (
      t('commentAt', { platform: place(item.review.platform) }) +
      (typeof title === 'string' && title.trim() ? t('underPost', { title }) : '')
    );
  }
  function assignment(item: DecisionItem) {
    const destinations =
      item.approval?.platforms.map((platform) =>
        t(t.has(`destination.${platform}`) ? `destination.${platform}` : 'destination.other')
      ) ?? [];
    const day = decisionDay(item.createdAt);
    const when = day
      ? t(`assignedDay.${day.kind}`, {
          date: new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'long' }).format(day.date),
        })
      : '';
    return [
      destinations.length
        ? new Intl.ListFormat(locale, { type: 'conjunction' }).format(destinations)
        : '',
      when ? t('assigned', { when }) : '',
    ]
      .filter(Boolean)
      .join(', ');
  }
  function rowTime(item: DecisionItem) {
    if (item.approval?.scheduledAt)
      return t('scheduledFor', { time: formatClockTime(item.approval.scheduledAt) });
    return decisionTimestamp(item.createdAt, locale, t);
  }
  function rowIcon(item: DecisionItem) {
    const platforms = item.approval?.platforms ?? [];
    if (platforms.length > 1)
      return (
        <span className="relative h-8 w-8 shrink-0" aria-hidden>
          <PlatformIcon platform={platforms[0]} className="absolute left-0 top-0 h-6 w-6" />
          <PlatformIcon
            platform={platforms[1]}
            className="absolute bottom-0 right-0 h-5 w-5 rounded-md ring-2 ring-block"
          />
        </span>
      );
    if (item.platform || platforms[0])
      return <PlatformIcon platform={item.platform || platforms[0]} />;
    return (
      <span
        aria-hidden
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-inner bg-brand text-brand-foreground"
      >
        {businessMark(business.data?.name || '')}
      </span>
    );
  }
  function automaticTitle(receipt: NonNullable<typeof queue.data>['automatic'][number]) {
    if (receipt.fact === 'address' || receipt.fact === 'hours')
      return t(`autoFact.${receipt.fact}`, { platform: place(receipt.platform) });
    if (receipt.authorName && receipt.rating)
      return t('autoPerson', {
        name: receipt.authorName,
        rating: receipt.rating,
        platform: place(receipt.platform),
      });
    return receipt.platform ? t('autoAt', { platform: place(receipt.platform) }) : t('autoReview');
  }
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const items = queue.data?.items ?? [];
  const selected = items.find((item) => item.id === selectedId) ?? items[0];
  function itemTitle(item: DecisionItem) {
    if (item.review) return item.review.authorName || t('review');
    if (item.approval?.post) {
      const day = item.approval.scheduledAt ? decisionDay(item.approval.scheduledAt) : null;
      const heading =
        day?.kind === 'tomorrow'
          ? t('postTomorrow')
          : day?.kind === 'today'
            ? t('postToday')
            : day
              ? t('postDate', {
                  date: new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'long' }).format(
                    day.date
                  ),
                })
              : t('newPost');
      return item.approval.title
        ? t('postTopic', { heading, topic: item.approval.title })
        : heading;
    }
    if (item.approval?.title) return item.approval.title;
    if (
      item.kind === 'drift' &&
      item.platform === 'yandex_business' &&
      item.drift?.driftFields.includes('schedule')
    )
      return t('hoursDifferent');
    return t(item.kind);
  }
  function itemSummary(item: DecisionItem) {
    if (item.review) return item.review.text;
    if (item.approval?.preview) return t('quote', { text: item.approval.preview });
    if (item.kind === 'approval_batch' && item.callCount)
      return t('approvalSummary', { count: item.callCount });
    if (item.kind === 'drift' && item.drift?.driftFields.length) {
      const values = drift.data?.find(
        (row) => row.platform === item.platform && row.externalId === item.drift?.externalId
      );
      const difference = firstScheduleDifference(
        values?.remoteValues?.schedule,
        values?.storedValues?.schedule
      );
      if (difference)
        return t('hoursDifferenceDetail', {
          day: tDays(difference.day),
          remote: difference.remote.closed
            ? t('closedDay')
            : `${difference.remote.open}–${difference.remote.close}`,
          stored: difference.stored.closed
            ? t('closedDay')
            : `${difference.stored.open}–${difference.stored.close}`,
        });
      const fields = item.drift.driftFields.filter((field) => tDrift.has(field));
      if (!fields.length) return null;
      return t('driftSummary', {
        count: item.drift.driftFields.length,
        fields: fields.map((field) => tDrift(field)).join(', '),
      });
    }
    return null;
  }
  function target(item: DecisionItem) {
    if (item.kind === 'approval_batch') return `/chat/${item.conversationId}`;
    if (item.kind === 'connection') return `/integrations?reconnect=${item.platform}`;
    return '/business#comparison';
  }
  return (
    <div className="space-y-title-gap px-gutter pb-10 pt-2 xl:px-gutter-desktop xl:pt-group">
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
          {new Intl.DateTimeFormat(locale, { weekday: 'long', day: 'numeric', month: 'long' })
            .format(new Date())
            .replace(/^./u, (letter) => letter.toLocaleUpperCase(locale))}
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
                <>
                  <ReviewDecision
                    key={`${businessId}:${selected.id}`}
                    review={selected.review}
                    businessId={businessId}
                    businessName={business.data?.name}
                  />
                  {selected.platform === 'yandex_business' &&
                    Number.isFinite(rating) &&
                    rating > 0 &&
                    Number.isFinite(ratingCount) &&
                    ratingCount > 0 && (
                      <div className="mt-5 hidden gap-5 xl:grid xl:grid-cols-2">
                        <section>
                          <h3 className="flex items-center gap-2 font-semibold">
                            <PlatformIcon platform="yandex_business" className="h-4 w-4" />
                            {t('mapsPresence')}
                          </h3>
                          <p className="mt-2 flex items-center gap-2">
                            <strong className="text-[28px] leading-8">
                              {new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(
                                rating
                              )}
                            </strong>
                            <span className="text-star" aria-hidden>
                              {'★★★★★'}
                            </span>
                          </p>
                          <p className="mt-1 text-meta text-ink-soft">
                            {t('ratingCount', { count: ratingCount })}
                          </p>
                        </section>
                        <section>
                          <h3 className="font-semibold">{t('editHeading')}</h3>
                          <p className="mt-2 text-reading text-ink-soft">{t('editHint')}</p>
                        </section>
                      </div>
                    )}
                </>
              ) : (
                <article className="space-y-4 rounded-block bg-block p-block md:p-block-desktop">
                  <h2 className="text-block-title">{itemTitle(selected)}</h2>
                  <p className="text-reading">
                    {t(`${selected.kind}Body`, { count: selected.callCount ?? 0 })}
                  </p>
                  {selected.approval && (
                    <p className="text-reading text-ink-soft">
                      {assignment(selected)}
                      {rowTime(selected) ? ` · ${rowTime(selected)}` : ''}
                    </p>
                  )}
                  {itemSummary(selected) && (
                    <blockquote className="text-reading">{itemSummary(selected)}</blockquote>
                  )}
                  {selected.drift && (
                    <p className="text-meta text-ink-soft">
                      {selected.drift.driftFields
                        .map((field) => (tDrift.has(field) ? tDrift(field) : ''))
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
                        {rowIcon(item)}
                        <span className="min-w-0 flex-1">
                          <span className="flex items-start justify-between gap-2">
                            <span className="min-w-0 font-semibold">{itemTitle(item)}</span>
                            {rowTime(item) && (
                              <time
                                dateTime={item.approval?.scheduledAt || item.createdAt}
                                className="shrink-0 text-[13px] leading-5 text-ink-soft"
                              >
                                {rowTime(item)}
                              </time>
                            )}
                          </span>
                          {item.review && (
                            <span className="mt-0.5 block text-meta text-ink-soft">
                              {item.review.rating > 0 && (
                                <span aria-hidden className="mr-1">
                                  {Array.from({ length: MAX_RATING }, (_, index) => index + 1).map(
                                    (star) => (
                                      <span
                                        key={star}
                                        className={
                                          star <= item.review!.rating
                                            ? 'text-star'
                                            : 'text-ink-faint'
                                        }
                                      >
                                        ★
                                      </span>
                                    )
                                  )}
                                </span>
                              )}
                              {sourceLabel(item)}
                            </span>
                          )}
                          {item.approval && (
                            <span className="mt-0.5 block text-meta text-ink-soft">
                              {assignment(item)}
                            </span>
                          )}

                          {itemSummary(item) && (
                            <span className="mt-1 line-clamp-2 text-[15px] leading-[22px]">
                              {itemSummary(item)}
                            </span>
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
                      <span className="flex-1">{automaticTitle(receipt)}</span>
                      <time className="text-meta text-ink-soft" dateTime={receipt.createdAt}>
                        {formatClockTime(receipt.createdAt)}
                      </time>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            {businessId && <InstructionComposer key={businessId} businessId={businessId} />}
          </aside>
        </div>
      )}
    </div>
  );
}
