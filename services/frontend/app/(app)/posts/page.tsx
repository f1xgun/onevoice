// app/(app)/posts/page.tsx — OneVoice (Linen) Posts page.
//
// Phase 19 / 19-12 — pilot adoption of the <DataTable> + useDataTableFilters
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
//   PageHeader → stat strip (4 cards) → filter bar (platform select, status
//   tabs, ⌘K search) → expandable posts table.
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { SkeletonMetricStrip } from '@/components/states';
import { DataTable, type Column } from '@/components/lists/DataTable';
import { useDataTableFilters } from '@/hooks/useDataTableFilters';
import { useDataTableSearch } from '@/hooks/useDataTableSearch';
import type { Post } from '@/types/post';

import { ChannelChip } from './_components/ChannelChip';
import { ExpandedPanel } from './_components/ExpandedPanel';
import { PostsEmpty } from './_components/PostsEmpty';
import { PostsSkeleton } from './_components/PostsSkeleton';
import { SearchField } from './_components/SearchField';
import { StatCard } from './_components/StatCard';
import { StatusBadge } from './_components/StatusBadge';
import { collectPlatforms, nextScheduledLabel } from './_helpers';

// ─── Constants ───────────────────────────────────────────────────────

type StatusKey = 'all' | 'published' | 'scheduled' | 'error';
type PlatformKey = 'all' | 'telegram' | 'vk' | 'yandex_business';

interface PostsFilters extends Record<string, string> {
  status: StatusKey;
  platform: PlatformKey;
}

const POSTS_GRID_TEMPLATE = '24px 1fr 140px 200px 160px 56px';
const POSTS_MIN_WIDTH = '620px';

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

  const { data: posts = [], isLoading } = useQuery<Post[]>({
    // Inline business-scoped key: ['businesses', bizId, 'posts', status,
    // platform]. Centralising this in QUERY_KEYS would need a new factory
    // that takes (bizId, status, platform); inline keeps the change local
    // to the RBAC migration.
    queryKey: ['businesses', activeBusinessId, 'posts', filters.status, filters.platform],
    queryFn: () => {
      const qs = queryString();
      const url = qs ? `${BIZ_API_PATHS.POSTS.ROOT}?${qs}` : BIZ_API_PATHS.POSTS.ROOT;
      return bizApi(activeBusinessId!)
        .get<{ posts?: Post[] }>(url)
        .then((r) => (r.data.posts ?? []) as Post[]);
    },
    // Gate the request on activeBusinessId so a null-keyed cache entry
    // never resolves data; matches the pattern from useConversations.
    enabled: !!activeBusinessId,
  });

  // Client-side text search over the (already server-filtered) list.
  const { query, setQuery, visibleRows } = useDataTableSearch<Post>({
    rows: posts,
    searchableFields: (p) => [p.content],
  });

  // TODO(api): aggregates should come from a /posts/stats endpoint so the
  // counts reflect the full collection, not just the current filter slice.
  // For now we derive them from `posts` (server-filtered) which matches the
  // tab counts the user is currently looking at.
  const counts = useMemo(() => {
    const by = (s: string) => posts.filter((p) => p.status === s).length;
    return {
      total: posts.length,
      published: by('published'),
      scheduled: by('scheduled'),
      error: by('error'),
    };
  }, [posts]);

  const postColumns = useMemo<Column<Post>[]>(
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
        cell: (p) => <StatusBadge status={p.status} />,
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
            <Button variant="primary" size="md">
              <Plus aria-hidden />
              {tPosts('createPost')}
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
            {/* Explicit ASCII id pins the Radix-generated id on the trigger
                so axe's `aria-valid-attr-value` (serious) never sees the
                «…» chars React 18 useId() injects into Radix's auto-id. */}
            <SelectTrigger
              id="posts-platform-select"
              aria-label={tCommon('allPlatforms')}
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

          {/* Explicit ASCII ids pin the Radix-generated id + aria-controls +
              aria-labelledby on each <Tabs.Trigger>/<Tabs.Content> pair. Same
              useId() encoding bug as the Select above. We also render empty
              <TabsContent> elements so the aria-controls IDREFs resolve to
              live DOM nodes (axe fails on dangling references when the rule
              format checks both attribute syntax + IDREF target presence). */}
          <Tabs
            id="posts-status-tabs"
            value={filters.status}
            onValueChange={(v) => setFilter('status', v as StatusKey)}
            className="-mx-1 overflow-x-auto px-1 sm:mx-0 sm:overflow-visible sm:px-0"
          >
            <TabsList className="h-8 bg-paper-sunken">
              <TabsTrigger
                value="all"
                id="posts-status-tabs-trigger-all"
                aria-controls="posts-status-tabs-content-all"
                className="h-7 text-[13px]"
              >
                {tPosts('tabs.all', { count: counts.total })}
              </TabsTrigger>
              <TabsTrigger
                value="published"
                id="posts-status-tabs-trigger-published"
                aria-controls="posts-status-tabs-content-published"
                className="h-7 text-[13px]"
              >
                {tPosts('tabs.published', { count: counts.published })}
              </TabsTrigger>
              <TabsTrigger
                value="scheduled"
                id="posts-status-tabs-trigger-scheduled"
                aria-controls="posts-status-tabs-content-scheduled"
                className="h-7 text-[13px]"
              >
                {tPosts('tabs.scheduled', { count: counts.scheduled })}
              </TabsTrigger>
              <TabsTrigger
                value="error"
                id="posts-status-tabs-trigger-error"
                aria-controls="posts-status-tabs-content-error"
                className="h-7 text-[13px]"
              >
                {tPosts('tabs.error', { count: counts.error })}
              </TabsTrigger>
            </TabsList>
            {/* Tabs on this page act as a filter chip group — the actual
                content lives in the <DataTable> below, NOT in Tabs.Content.
                But the ARIA tabs pattern requires aria-controls to resolve;
                we render empty, hidden TabsContent panels purely to anchor
                the IDREFs (and pin their ids to stable ASCII strings). */}
            <TabsContent
              value="all"
              id="posts-status-tabs-content-all"
              aria-labelledby="posts-status-tabs-trigger-all"
              className="sr-only"
            />
            <TabsContent
              value="published"
              id="posts-status-tabs-content-published"
              aria-labelledby="posts-status-tabs-trigger-published"
              className="sr-only"
            />
            <TabsContent
              value="scheduled"
              id="posts-status-tabs-content-scheduled"
              aria-labelledby="posts-status-tabs-trigger-scheduled"
              className="sr-only"
            />
            <TabsContent
              value="error"
              id="posts-status-tabs-content-error"
              aria-labelledby="posts-status-tabs-trigger-error"
              className="sr-only"
            />
          </Tabs>

          <span className="hidden flex-1 sm:inline" />

          <SearchField value={query} onChange={setQuery} />
        </div>

        {/* Table — full schema needs ~620 px; on narrow viewports we let
            the row scroll horizontally rather than collapsing columns,
            since each (status / platforms / date) carries information the
            operator scans at a glance. */}
        <DataTable<Post>
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
      </div>
    </>
  );
}
