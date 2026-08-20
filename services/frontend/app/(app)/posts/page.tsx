// app/(app)/posts/page.tsx — OneVoice (Linen) Posts page.
//
// pilot adoption of the <DataTable> + useDataTableFilters
// + useDataTableSearch composition primitives. Filter / search state moves
// out of inline useState into the sibling hooks; the table block becomes
// `<DataTable<Post>>` with a Column<Post>[] config.
//
// RBAC (plan 02-09): the /posts list is fetched via bizApi(activeBusinessId)
// so the request hits `/businesses/{id}/posts`. The query key is partitioned
// by business id to keep the React Query cache scoped per active business
// (so switching businesses can't surface cross-tenant posts).
//
// Layout per design_handoff_onevoice 2/mocks/mock-posts.jsx:
// PageHeader → stat strip (4 cards) → filter bar (platform select, status
// tabs, ⌘K search) → expandable posts table.
//
// Data contract is unchanged: GET /businesses/{id}/posts?status&platform →
// { posts: Post[] }. Aggregate counters in the stat strip are derived
// client-side from the returned list because the API doesn't expose summary
// endpoints yet — see `TODO(api)` markers below.

'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import { format } from 'date-fns';
import Link from 'next/link';
import { ChevronDown, ChevronRight, Plus } from 'lucide-react';

import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { getDateFnsLocale } from '@/lib/dateFnsLocale';
import type { Locale } from '@/lib/i18n/locales';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';
import { PageHeader } from '@/components/ui/page-header';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { SkeletonMetricStrip } from '@/components/states';
import { cn } from '@/lib/utils';
import { DataTable, type Column } from '@/components/lists/DataTable';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { useDataTableFilters } from '@/hooks/useDataTableFilters';
import { useDataTableSearch } from '@/hooks/useDataTableSearch';
import { useRadiogroupKeyboard } from '@/hooks/useRadiogroupKeyboard';
import type { Post } from '@/types/post';

import { BroadcastBadge } from './_components/BroadcastBadge';
import { ChannelChip } from './_components/ChannelChip';
import { ExpandedPanel } from './_components/ExpandedPanel';
import { PostsEmpty } from './_components/PostsEmpty';
import { PostsSkeleton } from './_components/PostsSkeleton';
import { SearchField } from './_components/SearchField';
import { StatCard } from './_components/StatCard';
import { StatusBadge } from './_components/StatusBadge';
import {
  collectPlatforms,
  mergeBroadcastGroups,
  nextScheduledLabel,
  type PostRow,
} from './_helpers';

// ─── Constants ───────────────────────────────────────────────────────

type StatusKey = 'all' | 'published' | 'scheduled' | 'error';
type PlatformKey = 'all' | 'telegram' | 'vk' | 'yandex_business';

interface PostsFilters extends Record<string, string> {
  status: StatusKey;
  platform: PlatformKey;
}

// Status column is sized for the widest broadcast badge («Опубликовано в N
// каналах»), not just the single-word per-post statuses.
const POSTS_GRID_TEMPLATE = '24px 1fr 190px 200px 160px 56px';
const POSTS_MIN_WIDTH = '670px';

// Status radiogroup option order — used both for rendering the chips and
// for the arrow-key navigation hook (which walks this list to decide which
// radio comes next).
const POSTS_STATUS_OPTIONS = [
  'all',
  'published',
  'scheduled',
  'error',
] as const satisfies readonly StatusKey[];

// ─── Page ────────────────────────────────────────────────────────────

export default function PostsPage() {
  const tPosts = useTranslations('posts');
  const tCommon = useTranslations('common');
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const canCreate = usePermission('content.create').allowed;

  const { filters, setFilter, queryString } = useDataTableFilters<PostsFilters>({
    defaultValue: { status: 'all', platform: 'all' },
  });

  const {
    data: posts = [],
    isLoading,
    isError,
    refetch,
  } = useQuery<Post[]>({
    queryKey: ['businesses', activeBusinessId, 'posts', filters.status, filters.platform],
    queryFn: () => {
      const qs = queryString();
      const url = qs ? `${BIZ_API_PATHS.POSTS.ROOT}?${qs}` : BIZ_API_PATHS.POSTS.ROOT;
      return bizApi(activeBusinessId!)
        .get<{ posts?: Post[] }>(url)
        .then((r) => (r.data.posts ?? []) as Post[]);
    },
    enabled: !!activeBusinessId,
  });

  const tableRows = useMemo(() => mergeBroadcastGroups(posts), [posts]);

  const { query, setQuery, visibleRows } = useDataTableSearch<PostRow>({
    rows: tableRows,
    searchableFields: (p) => [p.content],
  });

  const statusRadio = useRadiogroupKeyboard<StatusKey>({
    options: POSTS_STATUS_OPTIONS,
    value: filters.status,
    onValueChange: (v) => setFilter('status', v),
  });

  const counts = useMemo(() => {
    const by = (s: string) => posts.filter((p) => p.status === s).length;
    return {
      total: posts.length,
      published: by('published'),
      scheduled: by('scheduled'),
      error: by('error'),
    };
  }, [posts]);

  const postColumns = useMemo<Column<PostRow>[]>(
    () => [
      {
        id: 'expand',
        header: <span aria-hidden />,
        cell: (_post, ctx) => (
          <span aria-hidden className="text-ink-soft">
            {ctx.expanded ? (
              <ChevronDown className="size-4" />
            ) : (
              <ChevronRight className="size-4" />
            )}
          </span>
        ),
      },
      {
        id: 'content',
        header: tPosts('table.content'),
        cell: (p) => <span className="truncate text-sm text-ink">{p.content}</span>,
      },
      {
        id: 'status',
        header: tPosts('table.status'),
        cell: (p) =>
          p.broadcastChannels ? (
            <BroadcastBadge channels={p.broadcastChannels} />
          ) : (
            <StatusBadge status={p.status} />
          ),
      },
      {
        id: 'platforms',
        header: tPosts('table.platforms'),
        cell: (p) => {
          const platforms = collectPlatforms(p);
          return (
            <span className="flex flex-wrap items-center gap-1.5">
              {platforms.length === 0 ? (
                <span className="text-xs text-ink-faint">—</span>
              ) : (
                platforms.map((id) => <ChannelChip key={id} platform={id} />)
              )}
            </span>
          );
        },
      },
      {
        id: 'date',
        header: tPosts('table.date'),
        cell: (p) => {
          const dateIso = p.scheduledAt ?? p.publishedAt ?? p.createdAt;
          return (
            <MonoLabel tone="mid" className="text-[12px] normal-case tracking-normal">
              {format(new Date(dateIso), 'd MMM yyyy · HH:mm', { locale: dateFnsLocale })}
            </MonoLabel>
          );
        },
      },
      {
        id: 'more',
        header: <span aria-hidden />,
        cell: () => (
          <span aria-hidden className="text-right text-ink-soft">
            {tPosts('more')}
          </span>
        ),
      },
    ],
    [tPosts, dateFnsLocale]
  );

  return (
    <>
      <PageHeader
        title={tPosts('title')}
        sub={tPosts('subtitle')}
        actions={
          canCreate ? (
            <Button asChild variant="primary" size="md" title={tPosts('createPostTooltip')}>
              <Link href="/chat" aria-label={tPosts('createPostTooltip')}>
                <Plus aria-hidden />
                {tPosts('createPost')}
              </Link>
            </Button>
          ) : undefined
        }
      />

      <div className="px-4 pb-16 sm:px-12">
        {/* Stat strip — Linen static skeleton while the first /posts payload
            lands; the same 4-card geometry as the loaded state so the page
            doesn't reflow. */}
        {isLoading ? (
          <SkeletonMetricStrip count={3} />
        ) : (
          <section className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <StatCard
              label={tPosts('stats.publishedLabel')}
              value={String(counts.published)}
              hint={tPosts('stats.publishedHint')}
            />
            <StatCard
              label={tPosts('stats.scheduledLabel')}
              value={String(counts.scheduled)}
              hint={
                counts.scheduled > 0
                  ? tPosts('stats.scheduledHintNext', {
                      label: nextScheduledLabel(posts, dateFnsLocale),
                    })
                  : tPosts('stats.scheduledHintNone')
              }
            />
            <StatCard
              label={tPosts('stats.errorLabel')}
              value={String(counts.error)}
              hint={
                counts.error > 0 ? tPosts('stats.errorHintSome') : tPosts('stats.errorHintNone')
              }
              tone={counts.error > 0 ? 'danger' : 'neutral'}
            />
          </section>
        )}

        {/* Filter bar — stacks on narrow viewports so tabs don't overflow. */}
        <div className="mt-6 flex flex-col gap-3 rounded-md border border-line bg-paper-raised p-3 sm:flex-row sm:flex-wrap sm:items-center">
          <Select
            value={filters.platform}
            onValueChange={(v) => setFilter('platform', v as PlatformKey)}
          >
            <SelectTrigger
              id="posts-platform-select"
              aria-label={tPosts('platformLabel')}
              className="h-8 w-full text-sm sm:w-[180px]"
            >
              <SelectValue placeholder={tCommon('allPlatforms')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{tCommon('allPlatforms')}</SelectItem>
              <SelectItem value="telegram">{tPosts('platforms.telegram')}</SelectItem>
              <SelectItem value="vk">VK</SelectItem>
              <SelectItem value="yandex_business">{tPosts('platforms.yandexBusiness')}</SelectItem>
            </SelectContent>
          </Select>

          {/* Status filter — rendered as a radiogroup of chip buttons rather
              than Radix <Tabs>. Radix Tabs internally generates
              aria-controls IDREFs pointing at TabsContent panels that don't
              exist on this page, which trips axe-core's
              aria-valid-attr-value. A radiogroup is the semantically correct
              primitive for "pick one of N filters" and emits no IDREF attrs. */}
          <div
            role="radiogroup"
            aria-label={tPosts('statusLabel')}
            onKeyDown={statusRadio.onKeyDown}
            className="-mx-1 inline-flex h-8 items-center justify-center gap-0.5 overflow-x-auto rounded-lg bg-paper-sunken p-1 px-1 text-muted-foreground sm:mx-0 sm:overflow-visible sm:px-0"
          >
            {(
              [
                ['all', tPosts('tabs.all', { count: counts.total })],
                ['published', tPosts('tabs.published', { count: counts.published })],
                ['scheduled', tPosts('tabs.scheduled', { count: counts.scheduled })],
                ['error', tPosts('tabs.error', { count: counts.error })],
              ] as [StatusKey, string][]
            ).map(([key, label]) => {
              const active = filters.status === key;
              return (
                <button
                  key={key}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => setFilter('status', key)}
                  {...statusRadio.getRadioProps(key)}
                  className={cn(
                    'duration-[120ms] inline-flex h-7 items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-[13px] font-medium ring-offset-background transition-[background,color,box-shadow] ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
                    active && 'bg-background text-foreground shadow'
                  )}
                >
                  {label}
                </button>
              );
            })}
          </div>

          <span className="hidden flex-1 sm:inline" />

          <SearchField value={query} onChange={setQuery} />
        </div>

        {/* Table — full schema needs ~620 px; on narrow viewports we let
            the row scroll horizontally rather than collapsing columns,
            since each (status / platforms / date) carries information the
            operator scans at a glance. */}
        {isError ? (
          <ListLoadError onRetry={refetch} />
        ) : (
          <DataTable<PostRow>
            columns={postColumns}
            rows={visibleRows}
            rowKey={(p) => p.id}
            gridTemplate={POSTS_GRID_TEMPLATE}
            minWidth={POSTS_MIN_WIDTH}
            isLoading={isLoading}
            skeleton={<PostsSkeleton />}
            empty={<PostsEmpty search={query} onResetSearch={() => setQuery('')} />}
            expandable={(post) => <ExpandedPanel post={post} />}
          />
        )}
      </div>
    </>
  );
}
