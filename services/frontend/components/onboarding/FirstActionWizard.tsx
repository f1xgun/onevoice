'use client';

// components/onboarding/FirstActionWizard.tsx — the 60-second first-action
// wizard. After a fresh connect it chains four endpoints that already exist —
// POST /reviews/refresh (fan-out), POST /reviews/batch-draft (auto-selects the
// pending backlog), GET /reviews (polled until a draft is ready) and PUT
// /reviews/{id}/reply (one-tap publish) — to land the operator on their first
// real AI action. It writes no new producer and mirrors the reviews page's
// query keys + mutations so both surfaces read one warm cache.
//
// Controlled: the parent owns `open`. This lets the getting-started checklist
// mount it and pass `open()` as its `onOpenWizard` seam in a later slice.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import Link from 'next/link';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations, useLocale } from 'next-intl';
import { toast } from 'sonner';
import { Loader2, Sparkles, Star } from 'lucide-react';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { localeToIntlTag, type Locale } from '@/lib/i18n/locales';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';
import { ChannelMark } from '@/components/ui/channel-mark';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog';
import { cn } from '@/lib/utils';
import type { Review } from '@/types/review';

// Matches the manual refresh budget on the reviews page: the backend caps the
// fan-out at ~90s; the headroom absorbs network slack so a slow-but-fine
// refresh doesn't surface as a fake timeout.
const REFRESH_TIMEOUT_MS = 120_000;

// Bounded draft poll. A draft is one metered LLM call; generation is usually a
// few seconds. We stop after this window and offer a retry/skip rather than
// spin forever.
const POLL_INTERVAL_MS = 3_000;
const POLL_TIMEOUT_MS = 60_000;

// The chat fallback route — a brand-new org with no reviews is pointed here to
// compose a post instead (the guided-compose path lands later).
const CHAT_HREF = '/chat';

const PLATFORM_CHANNEL_MARK: Record<string, string> = {
  yandex_business: 'Yandex.Business',
  yandex: 'Yandex',
  google: 'Google',
  google_business: 'Google',
  '2gis': '2GIS',
  telegram: 'Telegram',
  vk: 'VK',
};

const PLATFORMS_WITH_RATING = new Set([
  'yandex_business',
  'yandex',
  'google',
  'google_business',
  '2gis',
]);

// Phases of the wizard's run. `preparing` covers refresh + batch-draft +
// polling (one quiet spinner); the others are terminal outcomes the operator
// acts on.
type Phase = 'idle' | 'preparing' | 'ready' | 'empty' | 'failed' | 'done';

export interface FirstActionWizardProps {
  /** Parent-owned open state — the wizard is fully controlled. */
  open: boolean;
  /** Called when the operator closes the wizard (backdrop, X, or a CTA). */
  onClose: () => void;
}

export function FirstActionWizard({ open, onClose }: FirstActionWizardProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-[560px]">
        {open && <WizardBody onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

// Split so every query/effect only mounts while the dialog is open — closing
// the wizard unmounts the poll rather than leaving it running in the cache.
function WizardBody({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const t = useTranslations('gettingStarted.wizard');

  const [phase, setPhase] = useState<Phase>('idle');
  const [pollDeadline, setPollDeadline] = useState<number | null>(null);
  const startedRef = useRef(false);

  const refreshMutation = useMutation({
    mutationFn: () =>
      bizApi(activeBusinessId!).post(BIZ_API_PATHS.REVIEWS.REFRESH, undefined, {
        timeout: REFRESH_TIMEOUT_MS,
      }),
  });

  const batchDraftMutation = useMutation({
    mutationFn: () =>
      bizApi(activeBusinessId!).post(BIZ_API_PATHS.REVIEWS.BATCH_DRAFT, { reviewIds: [] }),
  });

  const replyMutation = useMutation({
    mutationFn: ({ id, text }: { id: string; text: string }) => {
      if (!activeBusinessId) return Promise.reject(new Error('No active business'));
      return bizApi(activeBusinessId).put(BIZ_API_PATHS.REVIEWS.REPLY(id), { replyText: text });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_REVIEWS(activeBusinessId) });
      setPhase('done');
    },
    onError: () => toast.error(t('publishError')),
  });

  // The pending-review list, filtered server-side to the pending backlog. Same
  // shape as the reviews page's list query; nested under BUSINESS_REVIEWS so the
  // reply invalidation on the reviews page keeps this warm and vice-versa.
  const pendingQuery = useQuery<Review[]>({
    queryKey: [...QUERY_KEYS.BUSINESS_REVIEWS(activeBusinessId), 'pending'],
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(`${BIZ_API_PATHS.REVIEWS.ROOT}?reply_status=pending`)
        .then((r) => {
          const data = r.data as unknown;
          if (Array.isArray(data)) return data as Review[];
          const reviews = (data as { reviews?: Review[] } | null)?.reviews;
          return Array.isArray(reviews) ? reviews : [];
        }),
    enabled: phase === 'preparing' && !!activeBusinessId,
    // Poll while preparing; the effect below stops it once a draft resolves or
    // the deadline passes.
    refetchInterval: phase === 'preparing' ? POLL_INTERVAL_MS : false,
  });

  // The freshest pending review whose AI draft is ready to ship in one tap.
  const readyReview = useMemo(() => {
    const rows = pendingQuery.data ?? [];
    const ready = rows.filter(
      (r) =>
        r.replyStatus === 'pending' &&
        r.draftStatus === 'ready' &&
        !!r.draftReply &&
        r.draftReply.trim().length > 0
    );
    if (ready.length === 0) return null;
    return [...ready].sort(
      (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
    )[0];
  }, [pendingQuery.data]);

  // Whether any pending review is still generating a draft — keep polling for it.
  const anyGenerating = useMemo(
    () => (pendingQuery.data ?? []).some((r) => r.draftStatus === 'generating'),
    [pendingQuery.data]
  );

  const runChain = useCallback(async () => {
    if (!activeBusinessId) return;
    setPhase('preparing');
    setPollDeadline(null);
    try {
      await refreshMutation.mutateAsync();
    } catch {
      // A timed-out or failed fan-out isn't fatal: a prior sync may already
      // have reviews. Fall through to batch-draft + poll; only a total absence
      // of drafts routes to the empty/failed terminal states.
    }
    try {
      await batchDraftMutation.mutateAsync();
    } catch {
      setPhase('failed');
      return;
    }
    setPollDeadline(Date.now() + POLL_TIMEOUT_MS);
  }, [activeBusinessId, refreshMutation, batchDraftMutation]);

  // Kick the chain exactly once per mount (the dialog remounts this body each
  // open, so a fresh open re-runs).
  useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;
    void runChain();
  }, [runChain]);

  // Resolve the terminal phase from the poll. A ready draft wins immediately.
  // Otherwise, once the reviews list has SETTLED SUCCESSFULLY (isSuccess — NOT
  // !isPlaceholderData, which flips false on a fetch error while data stays
  // undefined and would read a default [] as a real "no reviews") and nothing
  // is still generating, an empty backlog routes to the compose-in-chat
  // fallback and the deadline passing routes to a retry/skip.
  useEffect(() => {
    if (phase !== 'preparing') return;
    if (readyReview) {
      setPhase('ready');
      return;
    }
    if (pollDeadline == null) return;
    const timedOut = Date.now() >= pollDeadline;
    if (anyGenerating && !timedOut) return;
    if (pendingQuery.isError) {
      if (timedOut) setPhase('failed');
      return;
    }
    if (pendingQuery.isSuccess) {
      const pendingCount = (pendingQuery.data ?? []).length;
      if (pendingCount === 0) {
        setPhase('empty');
        return;
      }
      if (timedOut) setPhase('failed');
    } else if (timedOut) {
      setPhase('failed');
    }
  }, [
    phase,
    readyReview,
    anyGenerating,
    pollDeadline,
    pendingQuery.isSuccess,
    pendingQuery.isError,
    pendingQuery.data,
  ]);

  // Re-evaluate the deadline even when no query update arrives (a stalled poll).
  useEffect(() => {
    if (phase !== 'preparing' || pollDeadline == null) return;
    const remaining = pollDeadline - Date.now();
    if (remaining <= 0) return;
    const timer = setTimeout(() => {
      void pendingQuery.refetch();
    }, remaining + 100);
    return () => clearTimeout(timer);
  }, [phase, pollDeadline, pendingQuery]);

  const retry = useCallback(() => {
    startedRef.current = true;
    void runChain();
  }, [runChain]);

  const publish = useCallback(() => {
    if (!readyReview?.draftReply) return;
    replyMutation.mutate({ id: readyReview.id, text: readyReview.draftReply.trim() });
  }, [readyReview, replyMutation]);

  return (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2 text-ink">
          <Sparkles className="size-4 text-ochre" aria-hidden />
          {t('title')}
        </DialogTitle>
        <DialogDescription className="text-ink-mid">{t('subtitle')}</DialogDescription>
      </DialogHeader>

      {phase === 'preparing' && <PreparingState label={t('preparing')} hint={t('preparingHint')} />}

      {phase === 'ready' && readyReview && (
        <ReadyState review={readyReview} publishing={replyMutation.isPending} />
      )}

      {phase === 'empty' && <EmptyState />}

      {phase === 'failed' && <FailedState />}

      {phase === 'done' && <DoneState />}

      <DialogFooter>
        {phase === 'ready' && readyReview && (
          <>
            <Button variant="ghost" onClick={onClose} disabled={replyMutation.isPending}>
              {t('later')}
            </Button>
            <Button variant="primary" onClick={publish} disabled={replyMutation.isPending}>
              {replyMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" aria-hidden />
              ) : null}
              {t('publish')}
            </Button>
          </>
        )}

        {(phase === 'empty' || phase === 'done') && (
          <>
            <Button variant="ghost" onClick={onClose}>
              {t('close')}
            </Button>
            <Button variant="primary" asChild onClick={onClose}>
              <Link href={CHAT_HREF}>{t('composeInChat')}</Link>
            </Button>
          </>
        )}

        {phase === 'failed' && (
          <>
            <Button variant="ghost" asChild onClick={onClose}>
              <Link href={CHAT_HREF}>{t('skipToChat')}</Link>
            </Button>
            <Button variant="primary" onClick={retry}>
              {t('retry')}
            </Button>
          </>
        )}

        {phase === 'preparing' && (
          <Button variant="ghost" onClick={onClose}>
            {t('cancel')}
          </Button>
        )}
      </DialogFooter>
    </>
  );
}

function PreparingState({ label, hint }: { label: string; hint: string }) {
  return (
    <div className="flex flex-col items-center gap-3 py-8 text-center">
      <Loader2 className="size-6 animate-spin text-ochre" aria-hidden />
      <p className="text-sm font-medium text-ink">{label}</p>
      <p className="max-w-sm text-[13px] text-ink-mid">{hint}</p>
    </div>
  );
}

function ReadyState({ review, publishing }: { review: Review; publishing: boolean }) {
  const t = useTranslations('gettingStarted.wizard');
  const locale = useLocale() as Locale;
  const channelMark = PLATFORM_CHANNEL_MARK[review.platform] ?? review.platform;
  const hasRating = PLATFORMS_WITH_RATING.has(review.platform) && review.rating > 0;

  return (
    <div className="space-y-4" aria-busy={publishing}>
      <div className="rounded-md border border-line-soft bg-paper-sunken px-4 py-3">
        <div className="mb-1.5 flex items-center gap-2">
          <ChannelMark name={channelMark} size={20} />
          <span className="text-sm font-medium text-ink">{review.authorName}</span>
          {hasRating && <StarRating rating={review.rating} />}
          <span className="ml-auto">
            <MonoLabel>{formatDate(review.createdAt, locale)}</MonoLabel>
          </span>
        </div>
        <p className="whitespace-pre-wrap text-sm leading-relaxed text-ink-mid">{review.text}</p>
      </div>

      <div className="rounded-md border border-line-soft bg-paper px-4 py-3">
        <div className="flex items-center justify-between gap-3">
          <MonoLabel tone="ochre">{t('draftLabel')}</MonoLabel>
          <span className="text-xs text-ink-soft">{t('draftHint')}</span>
        </div>
        <p className="mt-2 whitespace-pre-wrap text-sm leading-relaxed text-ink">
          {review.draftReply}
        </p>
      </div>
    </div>
  );
}

function EmptyState() {
  const t = useTranslations('gettingStarted.wizard');
  return (
    <div className="flex flex-col items-center gap-2 py-6 text-center">
      <div
        aria-hidden
        className="flex size-11 items-center justify-center rounded-full bg-paper-sunken"
      >
        <Sparkles className="size-5 text-ink-soft" />
      </div>
      <p className="text-sm font-medium text-ink">{t('empty.title')}</p>
      <p className="max-w-sm text-[13px] text-ink-mid">{t('empty.body')}</p>
    </div>
  );
}

function FailedState() {
  const t = useTranslations('gettingStarted.wizard');
  return (
    <div className="flex flex-col items-center gap-2 py-6 text-center">
      <p className="text-sm font-medium text-ink">{t('failed.title')}</p>
      <p className="max-w-sm text-[13px] text-ink-mid">{t('failed.body')}</p>
    </div>
  );
}

function DoneState() {
  const t = useTranslations('gettingStarted.wizard');
  return (
    <div className="flex flex-col items-center gap-2 py-6 text-center">
      <div
        aria-hidden
        className="flex size-11 items-center justify-center rounded-full bg-[var(--ov-success-soft)] text-[var(--ov-success)]"
      >
        <Sparkles className="size-5" />
      </div>
      <p className="text-sm font-medium text-ink">{t('done.title')}</p>
      <p className="max-w-sm text-[13px] text-ink-mid">{t('done.body')}</p>
    </div>
  );
}

function StarRating({ rating }: { rating: number }) {
  const t = useTranslations('gettingStarted.wizard');
  return (
    <div className="flex items-center gap-0.5" aria-label={t('ratingAria', { rating })}>
      {Array.from({ length: 5 }, (_, i) => (
        <Star
          key={i}
          aria-hidden
          className={cn(
            'size-3.5',
            i < rating ? 'fill-ochre text-ochre' : 'fill-transparent text-ink-faint'
          )}
        />
      ))}
    </div>
  );
}

function formatDate(iso: string, locale: Locale): string {
  try {
    return new Intl.DateTimeFormat(localeToIntlTag(locale), {
      day: 'numeric',
      month: 'short',
    }).format(new Date(iso));
  } catch {
    return iso;
  }
}
