// app/(app)/posts/page.tsx — OneVoice (Linen) Posts page rebuild.
//
// Layout per design_handoff_onevoice 2/mocks/mock-posts.jsx:
//   PageHeader → stat strip (4 cards) → filter bar (platform select, status
//   tabs, ⌘K search) → expandable posts table.
//
// Data contract is unchanged: GET /posts?status&platform → { posts: Post[] }.
// Aggregate counters in the stat strip are derived client-side from the
// returned list because the API doesn't expose summary endpoints yet — see
// `TODO(api)` markers below.
//
// Design rules (Brand Voice + Linen):
//   - Graphite primary for the headline action; ochre is reserved.
//   - Failure rows never live as a red badge alone — when a row is expanded
//     the failure surfaces as an explanatory strip inside the expanded panel
//     with a "Повторить" ghost action (text in danger color).
//   - Russian copy is calm, no exclamations, verb-first on actions.

'use client';

import { Fragment, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { format } from 'date-fns';
import { ru } from 'date-fns/locale';
import { ChevronDown, ChevronRight, FileText, Plus, Search } from 'lucide-react';

import { api } from '@/lib/api';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ChannelMark, type ChannelName } from '@/components/ui/channel-mark';
import { Input } from '@/components/ui/input';
import { MonoLabel } from '@/components/ui/mono-label';
import { PageHeader } from '@/components/ui/page-header';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import type { Post } from '@/types/post';

// ─── Constants ───────────────────────────────────────────────────────

type StatusKey = 'all' | 'published' | 'scheduled' | 'error';
type PlatformKey = 'all' | 'telegram' | 'vk' | 'yandex_business';

const statusLabel: Record<string, string> = {
  draft: 'Черновик',
  scheduled: 'Запланирован',
  published: 'Опубликован',
  error: 'Ошибка',
};

// Backend platform id → display name used by ChannelMark / labels.
const platformDisplay: Record<string, ChannelName> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Yandex.Business',
};

const platformShort: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Яндекс',
};

// ─── Page ────────────────────────────────────────────────────────────

export default function PostsPage() {
  const [status, setStatus] = useState<StatusKey>('all');
  const [platform, setPlatform] = useState<PlatformKey>('all');
  const [search, setSearch] = useState('');
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const { data: posts = [], isLoading } = useQuery<Post[]>({
    queryKey: ['posts', status, platform],
    queryFn: () => {
      const params = new URLSearchParams();
      if (status !== 'all') params.set('status', status);
      if (platform !== 'all') params.set('platform', platform);
      return api.get(`/posts?${params}`).then((r) => (r.data.posts ?? []) as Post[]);
    },
  });

  // Client-side text search over the (already server-filtered) list.
  const visiblePosts = useMemo(() => {
    if (!search.trim()) return posts;
    const q = search.trim().toLowerCase();
    return posts.filter((p) => p.content.toLowerCase().includes(q));
  }, [posts, search]);

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

  return (
    <>
      <PageHeader
        title="Посты"
        sub="Все публикации на подключённых каналах. Один пост — везде сразу."
        actions={
          <Button variant="primary" size="md">
            <Plus aria-hidden />
            Создать пост
          </Button>
        }
      />

      <div className="px-12 pb-16">
        {/* Stat strip */}
        <section className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatCard
            label="Опубликовано"
            value={String(counts.published)}
            hint="за всё время"
          />
          <StatCard
            label="Запланировано"
            value={String(counts.scheduled)}
            hint={
              counts.scheduled > 0
                ? `ближайшая — ${nextScheduledLabel(posts)}`
                : 'нет запланированных'
            }
          />
          <StatCard
            label="С ошибкой"
            value={String(counts.error)}
            hint={counts.error > 0 ? 'требуют внимания' : 'всё чисто'}
            tone={counts.error > 0 ? 'danger' : 'neutral'}
          />
          <StatCard
            label="Охват за 30 дней"
            value="—"
            hint="скоро"
            tone="muted"
            // TODO(api): backend doesn't return reach metrics yet. Render a
            // placeholder so the strip stays visually balanced.
          />
        </section>

        {/* Filter bar */}
        <div className="mt-6 flex flex-wrap items-center gap-3 rounded-md border border-line bg-paper-raised p-3">
          <Select value={platform} onValueChange={(v) => setPlatform(v as PlatformKey)}>
            <SelectTrigger className="h-8 w-[180px] text-sm">
              <SelectValue placeholder="Все платформы" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Все платформы</SelectItem>
              <SelectItem value="telegram">Telegram</SelectItem>
              <SelectItem value="vk">VK</SelectItem>
              <SelectItem value="yandex_business">Яндекс.Бизнес</SelectItem>
            </SelectContent>
          </Select>

          <Tabs value={status} onValueChange={(v) => setStatus(v as StatusKey)}>
            <TabsList className="h-8 bg-paper-sunken">
              <TabsTrigger value="all" className="h-7 text-[13px]">
                Все · {counts.total}
              </TabsTrigger>
              <TabsTrigger value="published" className="h-7 text-[13px]">
                Опубликованы · {counts.published}
              </TabsTrigger>
              <TabsTrigger value="scheduled" className="h-7 text-[13px]">
                Запланированы · {counts.scheduled}
              </TabsTrigger>
              <TabsTrigger value="error" className="h-7 text-[13px]">
                Ошибки · {counts.error}
              </TabsTrigger>
            </TabsList>
          </Tabs>

          <span className="flex-1" />

          <SearchField value={search} onChange={setSearch} />
        </div>

        {/* Table */}
        <div className="mt-4 overflow-hidden rounded-md border border-line bg-paper-raised">
          <div className="grid grid-cols-[24px_1fr_140px_200px_160px_56px] gap-4 border-b border-line bg-paper-sunken px-5 py-3">
            <span aria-hidden />
            <MonoLabel>Контент</MonoLabel>
            <MonoLabel>Статус</MonoLabel>
            <MonoLabel>Платформы</MonoLabel>
            <MonoLabel>Дата</MonoLabel>
            <span aria-hidden />
          </div>

          {isLoading && <PostsSkeleton />}

          {!isLoading && visiblePosts.length === 0 && (
            <EmptyState hasSearch={Boolean(search.trim())} />
          )}

          {!isLoading &&
            visiblePosts.map((post, i) => (
              <PostRow
                key={post.id}
                post={post}
                last={i === visiblePosts.length - 1}
                expanded={expandedId === post.id}
                onToggle={() =>
                  setExpandedId((prev) => (prev === post.id ? null : post.id))
                }
              />
            ))}
        </div>
      </div>
    </>
  );
}

// ─── Sub-components ──────────────────────────────────────────────────

function StatCard({
  label,
  value,
  hint,
  tone = 'neutral',
}: {
  label: string;
  value: string;
  hint: string;
  tone?: 'neutral' | 'danger' | 'muted';
}) {
  const labelTone = tone === 'danger' ? 'ochre' : 'soft';
  return (
    <div className="rounded-md border border-line bg-paper-raised px-5 py-4">
      <MonoLabel
        tone={labelTone}
        className={tone === 'danger' ? 'text-[var(--ov-danger)]' : undefined}
      >
        {label}
      </MonoLabel>
      <div
        className={
          'mt-1 text-[28px] font-medium tracking-[-0.015em] ' +
          (tone === 'muted' ? 'text-ink-soft' : 'text-ink')
        }
      >
        {value}
      </div>
      <div className="mt-0.5 text-xs text-ink-soft">{hint}</div>
    </div>
  );
}

function SearchField({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className="relative inline-flex h-8 w-[260px] items-center">
      <Search
        aria-hidden
        className="pointer-events-none absolute left-3 size-3.5 text-ink-soft"
      />
      <Input
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Поиск по содержимому…"
        className="h-8 bg-paper-sunken pl-9 pr-12 text-[13px]"
      />
      <span
        aria-hidden
        className="pointer-events-none absolute right-2 rounded border border-line-soft bg-paper px-1.5 py-0.5 font-mono text-[10px] text-ink-soft"
      >
        ⌘K
      </span>
    </label>
  );
}

function PostsSkeleton() {
  return (
    <div className="divide-y divide-line-soft">
      {Array.from({ length: 5 }, (_, i) => (
        <div
          key={i}
          className="grid grid-cols-[24px_1fr_140px_200px_160px_56px] items-center gap-4 px-5 py-4"
        >
          <span aria-hidden />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-5 w-20 rounded-full" />
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-4 w-28" />
          <span aria-hidden />
        </div>
      ))}
    </div>
  );
}

function EmptyState({ hasSearch }: { hasSearch: boolean }) {
  return (
    <div className="flex flex-col items-center px-6 py-16 text-center">
      <FileText aria-hidden className="mb-3 size-9 text-ink-faint" />
      <p className="text-sm text-ink-mid">
        {hasSearch ? 'Ничего не нашли по этому запросу' : 'Постов пока нет'}
      </p>
      {!hasSearch && (
        <p className="mt-1 max-w-xs text-xs text-ink-soft">
          Создайте первый пост — OneVoice опубликует его на всех подключённых каналах.
        </p>
      )}
    </div>
  );
}

function PostRow({
  post,
  last,
  expanded,
  onToggle,
}: {
  post: Post;
  last: boolean;
  expanded: boolean;
  onToggle: () => void;
}) {
  const platforms = collectPlatforms(post);
  const dateIso = post.scheduledAt ?? post.publishedAt ?? post.createdAt;
  const dateLabel = format(new Date(dateIso), 'd MMM yyyy · HH:mm', { locale: ru });

  return (
    <div className={last ? '' : 'border-b border-line-soft'}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className="grid w-full grid-cols-[24px_1fr_140px_200px_160px_56px] items-center gap-4 px-5 py-3.5 text-left transition-colors hover:bg-paper-sunken/60"
      >
        <span aria-hidden className="text-ink-soft">
          {expanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
        </span>
        <span className="truncate text-sm text-ink">{post.content}</span>
        <span>
          <StatusBadge status={post.status} />
        </span>
        <span className="flex flex-wrap items-center gap-1.5">
          {platforms.length === 0 ? (
            <span className="text-xs text-ink-faint">—</span>
          ) : (
            platforms.map((id) => (
              <ChannelChip key={id} platform={id} />
            ))
          )}
        </span>
        <MonoLabel tone="mid" className="normal-case tracking-normal text-[12px]">
          {dateLabel}
        </MonoLabel>
        <span aria-hidden className="text-right text-ink-soft">
          ⋯
        </span>
      </button>

      {expanded && <ExpandedPanel post={post} />}
    </div>
  );
}

function ExpandedPanel({ post }: { post: Post }) {
  const results = post.platformResults ? Object.entries(post.platformResults) : [];
  // Aggregate failure: any platform-level error, or top-level status=error.
  const firstError = results.find(([, r]) => r.error);
  const failureMessage = firstError?.[1].error ?? friendlyTopLevelError(post);

  return (
    <div className="grid grid-cols-1 gap-6 px-[60px] pb-5 lg:grid-cols-[1fr_300px]">
      <div className="rounded-md border border-line-soft bg-paper p-4">
        <div className="whitespace-pre-wrap text-sm leading-relaxed text-ink">
          {post.content}
        </div>

        {post.mediaUrls && post.mediaUrls.length > 0 && (
          <div className="mt-3 flex flex-wrap gap-2">
            {post.mediaUrls.map((url, i) => (
              <MediaThumb key={`${url}-${i}`} url={url} index={i} />
            ))}
          </div>
        )}

        {failureMessage && (
          <div className="mt-3 flex items-center gap-3 rounded-sm border border-[var(--ov-danger-soft)] bg-[var(--ov-danger-soft)] px-3.5 py-2.5">
            <span
              aria-hidden
              className="size-1.5 shrink-0 rounded-full bg-[var(--ov-danger)]"
            />
            <span className="flex-1 text-sm text-[var(--ov-danger)]">
              {failureMessage}
            </span>
            <Button
              variant="ghost"
              size="sm"
              className="text-[var(--ov-danger)] hover:bg-[var(--ov-danger-soft)] hover:text-[var(--ov-danger)]"
              // TODO(api): wire to POST /posts/:id/retry once backend exposes it.
            >
              Повторить
            </Button>
          </div>
        )}
      </div>

      <div className="flex flex-col gap-2.5">
        <MonoLabel>Результаты</MonoLabel>
        {results.length === 0 ? (
          <div className="rounded-sm border border-line-soft bg-paper px-3 py-2 text-xs text-ink-soft">
            Пока без статистики
          </div>
        ) : (
          results.map(([platform, result]) => (
            <PlatformResultCard key={platform} platform={platform} result={result} />
          ))
        )}
        <div className="mt-1 flex flex-wrap gap-2">
          <Button variant="secondary" size="sm">
            Дублировать
          </Button>
          {firstLink(post) && (
            <Button variant="ghost" size="sm" asChild>
              <a href={firstLink(post) ?? undefined} target="_blank" rel="noopener noreferrer">
                Открыть
              </a>
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

function PlatformResultCard({
  platform,
  result,
}: {
  platform: string;
  result: NonNullable<Post['platformResults']>[string];
}) {
  const ok = !result.error && (result.status === 'published' || result.status === 'ok');
  const display = platformDisplay[platform] ?? (platformShort[platform] ?? platform);
  return (
    <div className="flex items-center gap-2.5 rounded-sm border border-line-soft bg-paper px-3 py-2">
      <ChannelMark name={display} size={20} />
      <span className="flex-1 truncate text-[13px] text-ink-mid">
        {ok ? (result.url ? 'Опубликовано' : platformShort[platform] ?? display) : (result.error ?? result.status)}
      </span>
      <span
        aria-hidden
        className={
          'size-1.5 shrink-0 rounded-full ' +
          (ok ? 'bg-[var(--ov-success)]' : 'bg-[var(--ov-ink-faint)]')
        }
      />
    </div>
  );
}

function ChannelChip({ platform }: { platform: string }) {
  const display = platformDisplay[platform] ?? (platformShort[platform] ?? platform);
  const short = platformShort[platform] ?? display;
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border border-line-soft bg-paper px-2 py-0.5 text-[11px] text-ink-mid">
      <ChannelMark name={display} size={14} />
      {short}
    </span>
  );
}

function StatusBadge({ status }: { status: string }) {
  const label = statusLabel[status] ?? status;
  switch (status) {
    case 'published':
      return <Badge tone="success" dot>{label}</Badge>;
    case 'scheduled':
      return <Badge tone="info" dot>{label}</Badge>;
    case 'error':
      return <Badge tone="danger" dot>{label}</Badge>;
    case 'draft':
    default:
      return <Badge tone="neutral" dot>{label}</Badge>;
  }
}

function MediaThumb({ url, index }: { url: string; index: number }) {
  const filename = useMemo(() => {
    try {
      const parsed = new URL(url, 'http://example.com');
      return parsed.pathname.split('/').filter(Boolean).pop() ?? `файл ${index + 1}`;
    } catch {
      return `файл ${index + 1}`;
    }
  }, [url, index]);
  return (
    <div className="flex items-center gap-2.5 rounded-sm bg-paper-sunken px-2.5 py-2">
      <span aria-hidden className="size-8 rounded-sm bg-paper-well" />
      <MonoLabel tone="mid">{filename}</MonoLabel>
    </div>
  );
}

// ─── Helpers ─────────────────────────────────────────────────────────

function collectPlatforms(post: Post): string[] {
  if (post.platformResults) {
    const keys = Object.keys(post.platformResults);
    if (keys.length > 0) return keys;
  }
  return [];
}

function firstLink(post: Post): string | null {
  if (!post.platformResults) return null;
  for (const r of Object.values(post.platformResults)) {
    if (r.url) return r.url;
  }
  return null;
}

function nextScheduledLabel(posts: Post[]): string {
  const upcoming = posts
    .filter((p) => p.status === 'scheduled' && p.scheduledAt)
    .map((p) => new Date(p.scheduledAt as string))
    .sort((a, b) => a.getTime() - b.getTime());
  if (upcoming.length === 0) return '—';
  return format(upcoming[0], 'd MMM', { locale: ru });
}

function friendlyTopLevelError(post: Post): string | null {
  if (post.status !== 'error') return null;
  // Backend doesn't currently return a top-level error string, so we offer a
  // plain-Russian fallback that points the user at the next step.
  return 'Не удалось опубликовать. Проверьте подключение каналов и попробуйте ещё раз.';
}
