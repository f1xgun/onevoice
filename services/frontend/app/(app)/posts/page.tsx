'use client';

import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import Link from 'next/link';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { cn } from '@/lib/utils';
import type { Post } from '@/types/post';
import { mergeBroadcastGroups, postLocalDay } from './_helpers';
import { PostsEmpty } from './_components/PostsEmpty';
import { PostsSkeleton } from './_components/PostsSkeleton';
import { MediaThumb } from './_components/MediaThumb';
import { PostActions } from './_components/PostActions';

const TIME_FORMAT: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' };

export default function PostsPage() {
  const t = useTranslations('posts');
  const locale = useLocale();
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const canCreate = usePermission('content.create').allowed;
  const [filter, setFilter] = useState('all');
  const {
    data: posts = [],
    isLoading,
    isError,
    refetch,
  } = useQuery<Post[]>({
    queryKey: ['businesses', businessId, 'posts'],
    queryFn: () =>
      bizApi(businessId!)
        .get<{ posts?: Post[] }>(BIZ_API_PATHS.POSTS.ROOT)
        .then((r) => r.data.posts ?? []),
    enabled: !!businessId,
  });
  const days = useMemo(() => {
    const groups = new Map<string, ReturnType<typeof mergeBroadcastGroups>>();
    for (const row of mergeBroadcastGroups(posts).sort((a, b) =>
      (b.scheduledAt ?? b.publishedAt ?? b.createdAt).localeCompare(
        a.scheduledAt ?? a.publishedAt ?? a.createdAt
      )
    )) {
      const members = row.broadcastGroupId
        ? posts.filter((p) => p.broadcastGroupId === row.broadcastGroupId)
        : [row];
      if (filter === 'scheduled' && !members.some((p) => p.status === 'scheduled')) continue;
      if (
        filter === 'error' &&
        !members.some(
          (p) =>
            p.status === 'error' ||
            p.status === 'uncertain' ||
            Object.values(p.platformResults ?? {}).some((r) => r.error)
        )
      )
        continue;
      const day = postLocalDay(row);
      groups.set(day, [...(groups.get(day) ?? []), row]);
    }
    return [...groups];
  }, [posts, filter]);

  return (
    <div className="max-w-[816px] p-gutter md:p-gutter-desktop">
      <header className="mb-3.5 flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">{t('title')}</h1>
        {canCreate && (
          <Button asChild>
            <Link
              href={`/chat?${new URLSearchParams({ prompt: t('feed.newPrompt'), businessId: businessId ?? '' })}`}
            >
              {t('feed.new')}
            </Link>
          </Button>
        )}
      </header>
      <div
        role="group"
        aria-label={t('statusLabel')}
        className="mb-7 inline-flex max-w-full rounded-inner bg-btn2 p-1"
      >
        {['all', 'scheduled', 'error'].map((key) => (
          <button
            type="button"
            key={key}
            aria-pressed={filter === key}
            onClick={() => setFilter(key)}
            className={cn(
              'min-h-11 rounded-inner px-3 text-[15px] font-medium',
              filter === key ? 'bg-paper-raised text-ink shadow-sm' : 'text-ink-soft'
            )}
          >
            {t(`feed.${key}`)}
          </button>
        ))}
      </div>
      {isError ? (
        <ListLoadError onRetry={refetch} />
      ) : isLoading ? (
        <PostsSkeleton />
      ) : days.length === 0 ? (
        <PostsEmpty search="" onResetSearch={() => setFilter('all')} />
      ) : (
        <div className="space-y-7">
          {days.map(([day, rows]) => (
            <section key={day}>
              <h2 className="mb-2.5 text-base font-semibold">
                {new Date(`${day}T12:00:00`).toLocaleDateString(locale, {
                  weekday: 'long',
                  day: 'numeric',
                  month: 'long',
                })}
              </h2>
              <div className="space-y-3">
                {rows.map((row) => {
                  const members = row.broadcastGroupId
                    ? posts.filter((p) => p.broadcastGroupId === row.broadcastGroupId)
                    : [row];
                  const results =
                    row.broadcastChannels?.map((c) => [c.platform, c.result] as const) ??
                    Object.entries(row.platformResults ?? {});
                  const failed = results.some(([, r]) => r.error || r.status === 'error');
                  return (
                    <article
                      key={row.id}
                      className={cn(
                        'grid grid-cols-[56px_minmax(0,1fr)] gap-x-4 gap-y-3 rounded-block bg-paper-raised p-4 md:grid-cols-[64px_minmax(0,1fr)_224px] md:p-5',
                        failed && 'border border-danger'
                      )}
                    >
                      <time
                        className="text-xl font-semibold"
                        dateTime={row.scheduledAt ?? row.publishedAt ?? row.createdAt}
                      >
                        {new Date(
                          row.scheduledAt ?? row.publishedAt ?? row.createdAt
                        ).toLocaleTimeString(locale, TIME_FORMAT)}
                      </time>
                      <div className="min-w-0">
                        <p className="whitespace-pre-wrap break-words text-base leading-6">
                          {row.content}
                        </p>
                        {!!row.mediaUrls?.length && (
                          <div className="my-3 flex flex-wrap gap-2">
                            {row.mediaUrls.map((url, i) => (
                              <MediaThumb key={`${url}-${i}`} url={url} index={i} />
                            ))}
                          </div>
                        )}
                        <div className="mt-2 space-y-1">
                          {results.length === 0 && (
                            <p className="text-sm text-ink-soft">
                              {t(
                                `feed.status.${['published', 'scheduled', 'error', 'uncertain', 'deleted', 'canceled', 'processing'].includes(row.status) ? row.status : 'pending'}`
                              )}
                            </p>
                          )}
                          {results.map(([platform, result], i) => {
                            const memberStatus =
                              members.find((post) => post.platformResults?.[platform] === result)
                                ?.status ?? row.status;
                            const status = ['canceled', 'processing', 'uncertain'].includes(
                              memberStatus
                            )
                              ? memberStatus
                              : result.status;
                            return (
                              <div
                                key={`${platform}-${i}`}
                                className={cn(
                                  'flex items-start gap-2 text-[15px] leading-[22px]',
                                  result.error && 'text-danger'
                                )}
                              >
                                <PlatformIcon platform={platform} className="mt-1 size-4" />
                                <span className="min-w-0 flex-1 break-words">
                                  {t.has(`platformShort.${platform}`)
                                    ? t(`platformShort.${platform}`)
                                    : platform}{' '}
                                  —{' '}
                                  {result.error ||
                                    t(
                                      `feed.status.${['published', 'scheduled', 'error', 'uncertain', 'deleted', 'canceled', 'processing'].includes(status) ? status : status === 'ok' ? 'published' : 'pending'}`
                                    )}
                                </span>
                                {result.url && !result.error && (
                                  <a
                                    href={result.url}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    className="shrink-0 text-ink-soft underline"
                                  >
                                    {t('feed.open')}
                                  </a>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      </div>
                      <div className="col-span-2 md:col-span-1">
                        <PostActions key={`${businessId}-${row.id}`} posts={members} />
                      </div>
                    </article>
                  );
                })}
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
