'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import { ActionButton } from '@/components/design-system/ActionButton';
import { AppTextarea } from '@/components/design-system/AppInput';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { usePlatformMeta, type PlatformId } from '@/lib/platforms';
import { useBusinessVoice } from '@/lib/hooks/useBusinessVoice';
import { businessMark } from '@/lib/business-display';
import { decisionTimestamp } from '@/lib/decision-display';
import { usePermission } from '@/lib/hooks/usePermission';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { decisionQueueKey } from '@/hooks/useDecisionQueue';
import type { Review } from '@/types/review';
import { platformHasRating } from '../reviews/_lib/reviewPriority';

const MAX_REPLY = 4000;
const MAX_RATING = 5;
const schema = z.object({ reply: z.string().trim().min(1).max(MAX_REPLY) });
interface ReviewDecisionProps {
  review: Review;
  businessId: string;
  businessName?: string;
}
export function ReviewDecision({ review, businessId, businessName }: ReviewDecisionProps) {
  const t = useTranslations('inbox');
  const locale = useLocale();
  const qc = useQueryClient();
  const voice = useBusinessVoice(businessId);
  const platformMeta = usePlatformMeta();
  const permission = usePermission('content.update');
  const canWrite = permission.allowed && !permission.isLoading && !permission.isError;
  const failed = review.replyStatus === 'error';
  const signature = voice.data?.replySignature.trim();
  const rawDraft = review.draftReply ?? '';
  const draft =
    signature && rawDraft.trimEnd().endsWith(signature)
      ? [rawDraft.trimEnd().slice(0, -signature.length).trimEnd(), signature]
          .filter(Boolean)
          .join('\n\n')
      : rawDraft;
  const source = failed ? (review.replyText ?? '') : draft;
  const platform = platformMeta[review.platform as PlatformId]?.fullLabel || t('platformGeneric');
  const sourceWhen = decisionTimestamp(review.createdAt, locale, t, true);
  const destination = t(
    t.has(`place.${review.platform}`) ? `place.${review.platform}` : 'place.other'
  );
  const [editing, setEditing] = useState(false);
  const {
    register,
    handleSubmit,
    getValues,
    reset,
    watch,
    formState: { errors, isDirty },
  } = useForm<{ reply: string }>({
    resolver: zodResolver(schema),
    defaultValues: { reply: source },
  });
  useEffect(() => {
    if (failed || !isDirty) reset({ reply: source });
  }, [source, failed, isDirty, reset]);
  const mutation = useMutation({
    mutationFn: async (action: 'send' | 'retry' | 'draft' | 'skip') => {
      const api = bizApi(businessId);
      if (action === 'send')
        return api.put(BIZ_API_PATHS.REVIEWS.REPLY(review.id), {
          replyText: getValues('reply').trim(),
        });
      if (action === 'retry') return api.post(BIZ_API_PATHS.REVIEWS.REPLY_RETRY(review.id));
      if (action === 'skip') return api.post(`/reviews/${review.id}/skip`);
      await api.post(`/reviews/${review.id}/regenerate`);
      return (await api.get<Review>(`/reviews/${review.id}`)).data;
    },
    onSuccess: (data, action) => {
      if (action === 'draft' && data && 'draftReply' in data) {
        reset({ reply: data.draftReply ?? '' });
        setEditing(false);
      }
      void qc.invalidateQueries({ queryKey: decisionQueueKey(businessId) });
      void qc.invalidateQueries({ queryKey: ['businesses', businessId, 'reviews'] });
    },
  });
  return (
    <article className="space-y-4 rounded-block bg-block p-block md:p-block-desktop">
      <header className="flex items-start gap-3">
        <span
          aria-hidden
          className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-avatar font-semibold text-avatar-ink"
        >
          {review.authorName
            .trim()
            .split(/\s+/)
            .slice(0, 2)
            .map((part) => part[0])
            .join('')
            .toUpperCase()}
        </span>
        <div>
          <h2 className="text-reading font-semibold">{review.authorName}</h2>
          {platformHasRating(review.platform) && review.rating > 0 && (
            <p className="text-meta">
              <span className="text-star" aria-hidden>
                {Array.from({ length: MAX_RATING }, (_, index) => (
                  <span key={index} className={index < review.rating ? 'text-star' : 'text-line'}>
                    {'★'.repeat(1)}
                  </span>
                ))}
              </span>{' '}
              {t('rating', { rating: review.rating })}
            </p>
          )}
          <p className="flex items-center gap-2 text-meta text-ink-soft">
            <PlatformIcon platform={review.platform} className="h-head-sm w-head-sm" />
            {sourceWhen ? t('sourceWhen', { platform, when: sourceWhen }) : platform}
          </p>
        </div>
      </header>
      <p className="whitespace-pre-wrap text-review-mobile md:text-review">{review.text}</p>
      <form
        onSubmit={handleSubmit(() => {
          if (canWrite) mutation.mutate(failed ? 'retry' : 'send');
        })}
        className="space-y-4"
      >
        <div className="space-y-3 rounded-inner bg-brand-tone p-block md:p-block-desktop">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span
              aria-hidden
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-inner bg-brand text-sm font-semibold text-brand-foreground"
            >
              {businessMark(businessName || '')}
            </span>
            <strong className="text-base font-semibold">{businessName || t('business')}</strong>
            <label htmlFor={`draft-${review.id}`} className="text-meta lowercase text-ink-soft">
              {t(failed ? 'failedReply' : 'draft')}
            </label>
          </div>
          {(!source || editing) && !failed ? (
            <AppTextarea
              id={`draft-${review.id}`}
              autoFocus={editing}
              {...register('reply')}
              readOnly={!canWrite}
              disabled={mutation.isPending}
              aria-invalid={!!errors.reply}
              aria-describedby={errors.reply ? `draft-error-${review.id}` : undefined}
              className="!min-h-40 resize-none !border-transparent !bg-transparent !p-0 !text-response"
            />
          ) : canWrite && !failed ? (
            <button
              type="button"
              aria-label={t('editDraft')}
              onClick={() => setEditing(true)}
              className="w-full whitespace-pre-wrap text-left text-response"
            >
              {watch('reply') ?? source}
            </button>
          ) : (
            <p className="whitespace-pre-wrap text-response">{source}</p>
          )}
          {errors.reply && (
            <p id={`draft-error-${review.id}`} role="alert" className="text-meta text-danger">
              {t('replyRequired')}
            </p>
          )}
          {review.draftStatus === 'generating' && (
            <p role="status" className="text-meta text-ink-soft">
              {t('generating')}
            </p>
          )}
          {review.draftStatus === 'failed' && (
            <p className="text-meta text-danger">{t('draftFailed')}</p>
          )}
        </div>
        {mutation.isError && (
          <p role="alert" className="text-meta text-danger">
            {t('actionError')}
          </p>
        )}
        {permission.isLoading ? (
          <p role="status" className="text-meta text-ink-soft">
            {t('permissionLoading')}
          </p>
        ) : permission.isError ? (
          <PermissionLoadError onRetry={permission.refetch} />
        ) : canWrite ? (
          <div className="grid grid-cols-2 gap-2 sm:flex">
            {failed && !source.trim() ? (
              <ActionButton asChild variant="secondary">
                <Link href="/reviews">{t('allReviews')}</Link>
              </ActionButton>
            ) : (
              <ActionButton
                type="submit"
                aria-label={`${t(failed ? 'retryReply' : 'send')} ${destination}`}
                disabled={mutation.isPending || review.draftStatus === 'generating'}
                className="col-span-2 w-full sm:w-auto sm:flex-1"
              >
                <span className="flex flex-col leading-5">
                  <span>{t(failed ? 'retryReply' : 'send')}</span>
                  <span className="text-[13px] font-normal leading-4">{destination}</span>
                </span>
              </ActionButton>
            )}
            {!failed && (
              <>
                <ActionButton
                  type="button"
                  variant="secondary"
                  className="!px-2.5"
                  disabled={mutation.isPending || review.draftStatus === 'generating'}
                  onClick={() => mutation.mutate('draft')}
                >
                  {t('regenerate')}
                </ActionButton>
                <ActionButton
                  type="button"
                  variant="secondary"
                  className="!px-2.5"
                  disabled={mutation.isPending}
                  onClick={() => mutation.mutate('skip')}
                >
                  {t('skip')}
                </ActionButton>
              </>
            )}
          </div>
        ) : (
          <p className="text-meta text-ink-soft">{t('readOnly')}</p>
        )}
      </form>
    </article>
  );
}
