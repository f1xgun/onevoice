'use client';

// app/(app)/reviews/page.tsx — OneVoice (Linen) Reviews
// Customer feedback aggregated across connected platforms. Per brand voice
// "AI suggests, never commits": when a draft reply exists for a pending
// review, OneVoice surfaces it as an offer (paper-sunken sub-panel with
// "Образец ответа AI") that the operator must explicitly send or edit.
// No mock for this page — extrapolated from mock-states.jsx (empty state)
// and the patterns established in mock-posts.jsx (filter bar, stat strip).

import { useMemo, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Loader2, RefreshCw, Star } from 'lucide-react';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { REVIEW_STATUS_BADGES, type ReviewStatus } from '@/lib/constants/statuses';
import { Badge } from '@/components/ui/badge';

// Manual refresh fan-out budget. The backend caps total work at 90s; the
// extra headroom absorbs network slack so the request doesn't surface as
// a fake timeout in the UI before the agent actually finishes.
const REVIEWS_REFRESH_TIMEOUT_MS = 120_000;
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { EmptyReviews, type ReviewsEmptyMode } from '@/components/states';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';
import { ChannelMark } from '@/components/ui/channel-mark';
import { cn } from '@/lib/utils';
import type { Review } from '@/types/review';

// Platform id → display label + ChannelMark name. Reviews can land from
// any connected channel — Telegram/VK forward DMs that read as feedback,
// Yandex.Business is the canonical reviews surface.
const platformMeta: Record<string, { label: string; channel: string }> = {
  yandex_business: { label: 'Яндекс.Бизнес', channel: 'Yandex.Business' },
  yandex: { label: 'Яндекс', channel: 'Yandex' },
  google: { label: 'Google', channel: 'Google' },
  google_business: { label: 'Google Business', channel: 'Google' },
  '2gis': { label: '2ГИС', channel: '2GIS' },
  telegram: { label: 'Telegram', channel: 'Telegram' },
  vk: { label: 'ВКонтакте', channel: 'VK' },
};

function platformInfo(id: string): { label: string; channel: string } {
  return platformMeta[id] ?? { label: id, channel: id };
}

// Telegram channels and VK comments don't carry a 0–5 rating — the
// platform simply has no concept of one. Showing zero stars is misleading
// and pollutes the average. Only review-style platforms get a rating.
const platformsWithRating = new Set([
  'yandex_business',
  'yandex',
  'google',
  'google_business',
  '2gis',
]);
function platformHasRating(id: string): boolean {
  return platformsWithRating.has(id);
}

// Reply status → tone-mapped badge config — see lib/constants/statuses.
type StatusKey = ReviewStatus;
const statusBadge = REVIEW_STATUS_BADGES;

function StarRating({ rating, size = 16 }: { rating: number; size?: number }) {
  return (
    <div className="flex items-center gap-0.5" aria-label={`Оценка ${rating} из 5`}>
      {Array.from({ length: 5 }, (_, i) => (
        <Star
          key={i}
          aria-hidden
          style={{ width: size, height: size }}
          className={cn(
            'transition-colors',
            i < rating ? 'fill-ochre text-ochre' : 'fill-transparent text-ink-faint'
          )}
        />
      ))}
    </div>
  );
}

function ReviewSkeleton() {
  return (
    <div className="rounded-lg border border-line bg-paper-raised p-6">
      <div className="flex items-center gap-3">
        <Skeleton className="size-7 rounded-full bg-paper-sunken" />
        <Skeleton className="h-3.5 w-32 bg-paper-sunken" />
        <Skeleton className="h-3.5 w-20 bg-paper-sunken" />
        <span className="ml-auto">
          <Skeleton className="h-3 w-16 bg-paper-sunken" />
        </span>
      </div>
      <div className="mt-4 space-y-2">
        <Skeleton className="h-3 w-full bg-paper-sunken" />
        <Skeleton className="h-3 w-3/4 bg-paper-sunken" />
      </div>
    </div>
  );
}

// Format YYYY-MM-DD-ish ISO into "23 апр" style for the timestamp slot.
function formatReviewDate(iso: string): string {
  try {
    const d = new Date(iso);
    return new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short' }).format(d);
  } catch {
    return iso;
  }
}

export default function ReviewsPage() {
  const qc = useQueryClient();
  const [platform, setPlatform] = useState<string>('all');
  const [replyStatus, setReplyStatus] = useState<string>('all');
  const [replyDialog, setReplyDialog] = useState<Review | null>(null);
  const [replyText, setReplyText] = useState('');

  const { data: reviews = [], isLoading } = useQuery<Review[]>({
    queryKey: ['reviews', platform, replyStatus],
    queryFn: () => {
      const params = new URLSearchParams();
      if (platform !== 'all') params.set('platform', platform);
      if (replyStatus !== 'all') params.set('reply_status', replyStatus);
      return api.get(`${API_PATHS.REVIEWS.ROOT}?${params}`).then((r) => {
        // API shape: { reviews: Review[], total: number }. Older
        // callers expected a bare array — accept both for safety.
        const data = r.data as unknown;
        if (Array.isArray(data)) return data as Review[];
        const reviews = (data as { reviews?: Review[] } | null)?.reviews;
        return Array.isArray(reviews) ? reviews : [];
      });
    },
  });

  const replyMutation = useMutation({
    mutationFn: ({ id, text }: { id: string; text: string }) =>
      api.put(API_PATHS.REVIEWS.REPLY(id), { replyText: text }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['reviews'] });
      toast.success('Ответ отправлен');
      setReplyDialog(null);
      setReplyText('');
    },
    onError: () => toast.error('Не получилось отправить ответ'),
  });

  // Manual refresh — POSTs to /reviews/refresh which synchronously fans
  // out a sync request to every connected platform agent in parallel.
  // The 120s timeout accommodates Yandex.Business RPA scraping, which can
  // take up to ~60s; the backend caps total work at 90s per call.
  const refreshMutation = useMutation({
    mutationFn: () =>
      api.post(API_PATHS.REVIEWS.REFRESH, undefined, { timeout: REVIEWS_REFRESH_TIMEOUT_MS }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['reviews'] });
      toast.success('Отзывы обновлены');
    },
    onError: () => toast.error('Не удалось обновить отзывы'),
  });

  // Stats are computed from the loaded slice — they reflect what the
  // operator currently sees, not a global count. That keeps the strip
  // honest under platform/status filters.
  const stats = useMemo(() => {
    const total = reviews.length;
    const pending = reviews.filter((r) => r.replyStatus === 'pending').length;
    // Only include reviews from platforms that actually carry a rating —
    // VK comments and Telegram messages have rating=0 by default which
    // would otherwise drag the average toward zero.
    const ratings = reviews
      .filter((r) => platformHasRating(r.platform) && r.rating > 0)
      .map((r) => r.rating);
    const avg = ratings.length > 0 ? ratings.reduce((a, b) => a + b, 0) / ratings.length : null;
    return { total, pending, avg };
  }, [reviews]);

  function openReply(review: Review, prefill?: string) {
    setReplyDialog(review);
    // Edit-prefill priority: explicit prefill > AI draft > already-sent reply.
    // The drafter writes draftReply only on pending reviews, so once the
    // operator sends, draftReply is unset and replyText fills the slot.
    setReplyText(prefill ?? review.draftReply ?? review.replyText ?? '');
  }

  function sendDraftAsIs(review: Review) {
    const draft = review.draftReply?.trim();
    if (!draft) return;
    replyMutation.mutate({ id: review.id, text: draft });
  }

  return (
    <>
      <PageHeader
        title="Отзывы"
        sub="Здесь собираются отзывы клиентов с подключённых каналов. OneVoice предложит образец ответа — отправлять решаете вы."
      />

      <div className="px-4 pb-10 sm:px-12 sm:pb-16">
        {/* Stat strip — three quiet metrics. No celebratory tone. */}
        <div className="mb-6 grid grid-cols-1 gap-3 sm:grid-cols-3">
          <StatCell
            label="Ждут ответа"
            value={stats.pending}
            hint={stats.pending === 0 ? 'нет открытых' : 'требуют решения'}
          />
          <StatCell label="Всего в выборке" value={stats.total} hint="по выбранным фильтрам" />
          <StatCell
            label="Средняя оценка"
            value={stats.avg == null ? '—' : stats.avg.toFixed(1)}
            hint={stats.avg == null ? 'нет данных' : 'из 5'}
          />
        </div>

        {/* Filter bar — platform select + reply-status tabs. */}
        <div className="mb-6 flex flex-wrap items-center gap-3 rounded-md border border-line bg-paper-raised px-4 py-3">
          <Select value={platform} onValueChange={setPlatform}>
            <SelectTrigger className="h-9 w-[200px]">
              <SelectValue placeholder="Платформа" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Все платформы</SelectItem>
              <SelectItem value="yandex_business">Яндекс.Бизнес</SelectItem>
              <SelectItem value="google">Google</SelectItem>
              <SelectItem value="2gis">2ГИС</SelectItem>
              <SelectItem value="telegram">Telegram</SelectItem>
              <SelectItem value="vk">ВКонтакте</SelectItem>
            </SelectContent>
          </Select>

          <Tabs value={replyStatus} onValueChange={setReplyStatus}>
            <TabsList>
              <TabsTrigger value="all">Все</TabsTrigger>
              <TabsTrigger value="pending">Без ответа</TabsTrigger>
              <TabsTrigger value="replied">С ответом</TabsTrigger>
            </TabsList>
          </Tabs>

          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="ml-auto gap-1.5"
            onClick={() => refreshMutation.mutate()}
            disabled={refreshMutation.isPending}
            title="Запросить свежие отзывы у подключённых платформ"
          >
            {refreshMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <RefreshCw className="size-3.5" />
            )}
            {refreshMutation.isPending ? 'Обновляем…' : 'Обновить'}
          </Button>
        </div>

        {isLoading && (
          <div className="space-y-3 duration-200 animate-in fade-in">
            {Array.from({ length: 3 }, (_, i) => (
              <ReviewSkeleton key={i} />
            ))}
          </div>
        )}

        {!isLoading && reviews.length === 0 && <ReviewsEmptyState replyStatus={replyStatus} />}

        {!isLoading && reviews.length > 0 && (
          <div className="space-y-3 duration-300 animate-in fade-in">
            {reviews.map((review) => (
              <ReviewCard
                key={review.id}
                review={review}
                onSendDraft={() => sendDraftAsIs(review)}
                onEdit={() => openReply(review, review.draftReply ?? '')}
                onWriteOwn={() => openReply(review, '')}
                isSending={replyMutation.isPending && replyMutation.variables?.id === review.id}
              />
            ))}
          </div>
        )}
      </div>

      <Dialog open={!!replyDialog} onOpenChange={(open) => !open && setReplyDialog(null)}>
        <DialogContent className="sm:max-w-[520px]">
          <DialogHeader>
            <DialogTitle className="text-ink">Ответ на отзыв</DialogTitle>
          </DialogHeader>
          {replyDialog && (
            <div className="space-y-4">
              <div className="rounded-md border border-line-soft bg-paper-sunken px-4 py-3">
                <div className="mb-1.5 flex items-center gap-2">
                  <ChannelMark name={platformInfo(replyDialog.platform).channel} size={20} />
                  <span className="text-sm font-medium text-ink">{replyDialog.authorName}</span>
                  {platformHasRating(replyDialog.platform) && (
                    <StarRating rating={replyDialog.rating} size={14} />
                  )}
                </div>
                <p className="text-sm leading-relaxed text-ink-mid">{replyDialog.text}</p>
              </div>
              <div className="space-y-1.5">
                <MonoLabel>Ваш ответ</MonoLabel>
                <Textarea
                  value={replyText}
                  onChange={(e) => setReplyText(e.target.value)}
                  placeholder="Напишите ответ клиенту…"
                  rows={5}
                  className="resize-none"
                />
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setReplyDialog(null)}>
              Отмена
            </Button>
            <Button
              variant="primary"
              onClick={() =>
                replyDialog && replyMutation.mutate({ id: replyDialog.id, text: replyText })
              }
              disabled={!replyText.trim() || replyMutation.isPending}
            >
              {replyMutation.isPending ? 'Отправляем…' : 'Отправить'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

// ─── Subcomponents ─────────────────────────────────────────────────

function StatCell({
  label,
  value,
  hint,
}: {
  label: string;
  value: string | number;
  hint?: string;
}) {
  return (
    <div className="rounded-md border border-line bg-paper-raised px-5 py-4">
      <MonoLabel>{label}</MonoLabel>
      <div className="mt-1.5 text-[26px] font-medium leading-none tracking-[-0.015em] text-ink">
        {value}
      </div>
      {hint && <div className="mt-1.5 text-xs text-ink-soft">{hint}</div>}
    </div>
  );
}

function ReviewCard({
  review,
  onSendDraft,
  onEdit,
  onWriteOwn,
  isSending,
}: {
  review: Review;
  onSendDraft: () => void;
  onEdit: () => void;
  onWriteOwn: () => void;
  isSending: boolean;
}) {
  const meta = platformInfo(review.platform);
  const status =
    (review.replyStatus as StatusKey) in statusBadge ? (review.replyStatus as StatusKey) : 'read';
  const badge = statusBadge[status];

  // AI-draft surface conditions. Status semantics:
  //   draftStatus=ready  → show "Образец ответа AI" with send/edit actions
  //   draftStatus=generating → show a quiet "готовим черновик" block
  //   draftStatus=failed → fall through to the "write your own" fallback
  //   draftStatus=""|undefined → not yet attempted (also fallback)
  const draftReady =
    status === 'pending' &&
    review.draftStatus === 'ready' &&
    !!review.draftReply &&
    review.draftReply.trim().length > 0;
  const draftGenerating = status === 'pending' && review.draftStatus === 'generating';
  const hasSentReply = status === 'replied' && !!review.replyText;

  return (
    <article className="rounded-lg border border-line bg-paper-raised p-6 shadow-ov-1">
      {/* Header — channel mark, name, stars, date, status badge */}
      <header className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <ChannelMark name={meta.channel} size={22} />
        <span className="text-sm font-medium text-ink">{review.authorName}</span>
        <span aria-hidden className="size-1 rounded-full bg-ink-faint" />
        <span className="text-xs text-ink-soft">{meta.label}</span>
        {platformHasRating(review.platform) && <StarRating rating={review.rating} />}
        <span className="ml-auto flex items-center gap-3">
          <MonoLabel>{formatReviewDate(review.createdAt)}</MonoLabel>
          <Badge tone={badge.tone} dot>
            {badge.label}
          </Badge>
        </span>
      </header>

      {/* Review body */}
      <p className="mt-4 whitespace-pre-wrap text-[15px] leading-relaxed text-ink-mid">
        {review.text}
      </p>

      {/* Sent reply — quiet sub-panel, no actions */}
      {hasSentReply && (
        <div className="mt-4 rounded-md border border-line-soft bg-paper-sunken px-4 py-3">
          <MonoLabel>Отправленный ответ</MonoLabel>
          <p className="mt-1.5 text-sm leading-relaxed text-ink">{review.replyText}</p>
        </div>
      )}

      {/* AI draft — offered, not committed. Two actions: send / edit. */}
      {draftReady && (
        <div className="mt-4 rounded-md border border-line-soft bg-paper-sunken px-4 py-3">
          <div className="flex items-center justify-between gap-3">
            <MonoLabel tone="ochre">Образец ответа AI</MonoLabel>
            <span className="text-xs text-ink-soft">Можно отправить или отредактировать</span>
          </div>
          <p className="mt-2 text-sm leading-relaxed text-ink">{review.draftReply}</p>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button variant="primary" size="sm" onClick={onSendDraft} disabled={isSending}>
              {isSending ? 'Отправляем…' : 'Отправить'}
            </Button>
            <Button variant="ghost" size="sm" onClick={onEdit} disabled={isSending}>
              Отредактировать
            </Button>
          </div>
        </div>
      )}

      {/* Generating state — quiet hint, no actions until ready. */}
      {draftGenerating && (
        <div className="bg-paper-sunken/60 mt-4 flex items-center justify-between gap-3 rounded-md border border-dashed border-line px-4 py-3">
          <span className="text-sm text-ink-mid">Готовим образец ответа…</span>
          <Button variant="secondary" size="sm" onClick={onWriteOwn}>
            Ответить сейчас
          </Button>
        </div>
      )}

      {/* No-draft fallback: covers draft_status ∈ {"", "failed"} and any
          unexpected value. The operator can always author a reply by hand. */}
      {status === 'pending' && !draftReady && !draftGenerating && (
        <div className="bg-paper-sunken/60 mt-4 flex items-center justify-between gap-3 rounded-md border border-dashed border-line px-4 py-3">
          <span className="text-sm text-ink-mid">
            {review.draftStatus === 'failed'
              ? 'Не получилось подготовить образец. Можно ответить вручную.'
              : 'Образец ещё не подготовлен. Можно ответить вручную.'}
          </span>
          <Button variant="secondary" size="sm" onClick={onWriteOwn}>
            Написать ответ
          </Button>
        </div>
      )}
    </article>
  );
}

function ReviewsEmptyState({ replyStatus }: { replyStatus: string }) {
  // Tailor the copy to the active filter so the page doesn't claim
  // there are zero reviews when really we're just filtered to "pending".
  const mode: ReviewsEmptyMode =
    replyStatus === 'pending' ? 'pending' : replyStatus === 'replied' ? 'replied' : 'all';
  return <EmptyReviews mode={mode} />;
}
