'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChevronRight } from 'lucide-react';
import { z } from 'zod';
import { toast } from 'sonner';
import { ActionButton } from '@/components/design-system/ActionButton';
import {
  Sheet,
  SheetContent,
  SheetTitle,
  SheetDescription,
} from '@/components/design-system/AppSheet';
import { VoiceProfileSection } from '@/components/business/VoiceProfileSection';
import { DriftAlertSettings } from '@/components/integrations/DriftAlertSettings';
import { useBusinessVoice } from '@/lib/hooks/useBusinessVoice';
import { usePlatformFullLabels, isKnownPlatform } from '@/lib/platforms';
import type { Review } from '@/types/review';
import { VoiceToneSection } from '@/components/business/VoiceToneSection';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { bizApi } from '@/lib/api/business-api';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { useTools } from '@/lib/hooks/useTools';
import { useBusinessToolApprovals } from '@/lib/hooks/useBusinessToolApprovals';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { normalizeStoredTones } from '@/lib/tones';
import { cn } from '@/lib/utils';
import type { Business } from '@/types/business';

const MAX_RATING = 5;
const FOUR_STARS = 4;
const autopilotSchema = z.object({
  enabled: z.boolean(),
  minRating: z.number(),
  ignoreFourStar: z.boolean(),
  autoAnswerFacts: z.boolean(),
});
type Autopilot = z.infer<typeof autopilotSchema>;
type Editor = 'five' | 'four' | 'facts' | 'tone' | 'profile' | 'alerts' | null;

export default function RulesPage() {
  const t = useTranslations('rulesV2');
  const platformLabels = usePlatformFullLabels();
  const toneLabels = useTranslations('business.voiceTone.options');
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const permission = usePermission('business.update');
  const qc = useQueryClient();
  const [editor, setEditor] = useState<Editor>(null);
  const autopilot = useQuery({
    queryKey: ['business', businessId, 'reviewAutopilot'],
    queryFn: () =>
      bizApi(businessId!)
        .get('/review-autopilot')
        .then((r) => autopilotSchema.parse(r.data)),
    enabled: !!businessId,
  });
  const business = useQuery({
    queryKey: QUERY_KEYS.BUSINESS_PROFILE(businessId),
    queryFn: () =>
      bizApi(businessId!)
        .get<Business>('')
        .then((r) => r.data),
    enabled: !!businessId,
  });
  const voice = useBusinessVoice(businessId);
  const reviews = useQuery({
    queryKey: ['business', businessId, 'rulesReplies'],
    queryFn: async () => {
      const { data } = await bizApi(businessId!).get<Review[] | { reviews: Review[] }>('/reviews');
      return Array.isArray(data) ? data : data.reviews;
    },
    enabled: !!businessId,
  });
  const tools = useTools();
  const approvals = useBusinessToolApprovals(businessId ?? '');
  const save = useMutation({
    mutationFn: ({ id, config }: { id: string; config: Autopilot }) =>
      bizApi(id).put('/review-autopilot', config),
    onSuccess: (_response, variables) => {
      void qc.invalidateQueries({ queryKey: ['business', variables.id, 'reviewAutopilot'] });
      setEditor(null);
      toast.success(t('saved'));
    },
    onError: () => toast.error(t('failed')),
  });
  const config = autopilot.data;
  const profile = voice.data?.voiceProfile.trim() ?? '';
  const signature = voice.data?.replySignature.trim() ?? '';
  const tones = normalizeStoredTones(business.data?.settings?.voiceTone);
  const lastReply = reviews.data
    ?.filter((review) => review.replyStatus === 'replied' && review.replyText)
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0]?.replyText;
  const example =
    lastReply ||
    (business.isSuccess && tones.length
      ? t(tones.includes('warm') || tones.includes('friendly') ? 'warmExample' : 'shortExample')
      : '');
  const canEdit = permission.allowed && !permission.isError && !permission.isLoading;
  const fourMode = config?.ignoreFourStar
    ? 'skip'
    : config?.enabled && config.minRating <= FOUR_STARS
      ? 'auto'
      : 'manual';
  function update(mode: 'auto' | 'manual' | 'skip') {
    if (!config || !businessId || !canEdit) return;
    const next =
      editor === 'facts'
        ? { ...config, autoAnswerFacts: mode === 'auto' }
        : editor === 'five'
          ? {
              ...config,
              ignoreFourStar: config.ignoreFourStar ?? false,
              enabled: mode === 'auto',
              minRating: mode === 'auto' ? config.minRating : MAX_RATING,
            }
          : {
              ...config,
              ignoreFourStar: mode === 'skip',
              minRating: mode === 'auto' ? FOUR_STARS : MAX_RATING,
              enabled: mode === 'auto' ? true : config.enabled,
            };
    save.mutate({ id: businessId, config: next });
  }
  function row(title: string, value: string, onClick?: () => void, isEmpty = false) {
    return (
      <button
        type="button"
        disabled={!onClick}
        aria-label={`${title}: ${value}`}
        onClick={onClick}
        className="flex min-h-20 w-full items-center justify-between gap-4 px-5 py-4 text-left disabled:cursor-default"
      >
        <span>
          <span className="block text-base text-ink">{title}</span>
          <span className={cn('mt-1 block text-sm', isEmpty ? 'text-ink-soft' : 'text-ink-mid')}>
            {value}
          </span>
        </span>
        {onClick && <ChevronRight className="h-4 w-4 shrink-0 text-ink-mid" aria-hidden />}
      </button>
    );
  }
  function commentRows() {
    if (tools.isLoading || approvals.isLoading)
      return <p className="p-5 text-sm text-ink-mid">{t('loading')}</p>;
    if (tools.isError || approvals.isError)
      return (
        <p role="alert" className="p-5 text-sm text-danger">
          {t('loadError')}
        </p>
      );
    const comments = (tools.data ?? []).filter((tool) => /reply.*comment/.test(tool.name));
    if (!comments.length) return <p className="p-5 text-sm text-ink-mid">{t('none')}</p>;
    const modes = comments.map((tool) => ({
      tool,
      mode:
        tool.floor === 'forbidden'
          ? 'forbidden'
          : tool.floor === 'auto' &&
              approvals.data?.[tool.name] !== 'manual' &&
              config?.autoAnswerFacts
            ? 'factsAuto'
            : 'manual',
    }));
    return (
      <>
        {new Set(modes.map(({ mode }) => mode)).size === 1
          ? row(
              t('facts'),
              config ? t(modes[0].mode) : t('unknown'),
              config && canEdit ? () => setEditor('facts') : undefined
            )
          : modes.map(({ tool, mode }) => (
              <div key={tool.name}>
                {row(
                  t('platformFacts', {
                    platform: isKnownPlatform(tool.platform)
                      ? platformLabels[tool.platform]
                      : t('comments'),
                  }),
                  t(mode),
                  config && canEdit ? () => setEditor('facts') : undefined
                )}
              </div>
            ))}
        {row(t('otherComments'), t('manual'))}
      </>
    );
  }
  return (
    <>
      <header className="max-w-[816px] px-gutter pb-title-gap pt-group md:px-gutter-desktop">
        <h1 className="text-page-title">{t('title')}</h1>
        <p className="mt-1 text-ink-mid">{t('sub')}</p>
      </header>
      <div className="max-w-[816px] space-y-group px-gutter pb-group md:px-gutter-desktop">
        <section>
          <h2 className="font-semibold text-ink">{t('reviews')}</h2>
          <p className="mb-3 mt-1 text-sm text-ink-mid">{t('reviewsHint')}</p>
          {autopilot.isPending ? (
            <p role="status">{t('loading')}</p>
          ) : autopilot.isError ? (
            <div role="alert">
              <p>{t('loadError')}</p>
              <ActionButton onClick={() => autopilot.refetch()}>{t('retry')}</ActionButton>
            </div>
          ) : (
            <div className="overflow-hidden rounded-block bg-card">
              {row(
                t('five'),
                t(config?.enabled ? 'auto' : 'manual'),
                canEdit ? () => setEditor('five') : undefined
              )}
              {row(t('four'), t(fourMode), canEdit ? () => setEditor('four') : undefined)}
              {row(t('negative'), t('manual'))}
            </div>
          )}
          {permission.isError && <PermissionLoadError onRetry={permission.refetch} />}
          {!permission.isLoading && !permission.isError && !permission.allowed && (
            <p className="mt-2 text-sm text-ink-mid">{t('readOnly')}</p>
          )}
        </section>
        <section>
          <h2 className="font-semibold text-ink">{t('comments')}</h2>
          <p className="mb-3 mt-1 text-sm text-ink-mid">{t('commentsSummary')}</p>
          <div className="overflow-hidden rounded-block bg-card">{commentRows()}</div>
        </section>
        <section>
          <h2 className="mb-3 font-semibold text-ink">{t('posts')}</h2>
          <div className="overflow-hidden rounded-block bg-card">
            <Link
              href="/settings/tools"
              className="flex min-h-20 items-center justify-between gap-4 px-5 py-4"
            >
              <span>
                <span className="block text-base text-ink">{t('newPosts')}</span>
                <span className="mt-1 block text-sm text-ink-mid">{t('postsApproval')}</span>
              </span>
              <ChevronRight className="h-4 w-4 shrink-0 text-ink-mid" aria-hidden />
            </Link>
          </div>
        </section>
        <section>
          <h2 className="mb-3 font-semibold text-ink">{t('card')}</h2>
          <div className="overflow-hidden rounded-block bg-card">
            {row(t('cardChanged'), t('cardNotification'), () => setEditor('alerts'))}
          </div>
        </section>
        <section>
          <h2 className="font-semibold text-ink">{t('voice')}</h2>
          <p className="mb-3 mt-1 text-sm text-ink-mid">{t('voiceHint')}</p>
          {example && (
            <figure className="mb-block-gap rounded-block bg-card p-5">
              <blockquote className="whitespace-pre-line text-reading text-ink">
                {t('quote', { text: example })}
              </blockquote>
              <figcaption className="mt-2 text-meta text-ink-soft">
                {t(lastReply ? 'currentReply' : 'toneExample')}
              </figcaption>
            </figure>
          )}
          <div className="overflow-hidden rounded-block bg-card">
            {row(
              t('tone'),
              tones.map((tone) => toneLabels(tone)).join(', ') || t('noTone'),
              business.isSuccess ? () => setEditor('tone') : undefined
            )}
            {row(
              t('profile'),
              voice.isSuccess
                ? profile || t('profileEmpty')
                : t(voice.isError ? 'loadError' : 'loading'),
              () => setEditor('profile'),
              !profile
            )}
            {row(
              t('signature'),
              voice.isSuccess
                ? signature || t('profileEmpty')
                : t(voice.isError ? 'loadError' : 'loading'),
              () => setEditor('profile'),
              !signature
            )}
          </div>
          {business.isError && (
            <p role="alert" className="mt-2 text-danger">
              {t('loadError')}
            </p>
          )}
          <Link
            href="/settings/tools"
            className="mt-6 inline-flex min-h-11 items-center text-brand underline"
          >
            {t('advanced')}
          </Link>
        </section>
      </div>
      <Sheet
        open={editor !== null}
        onOpenChange={(open) => {
          if (!open) setEditor(null);
        }}
      >
        <SheetContent side="bottom">
          <SheetTitle>
            {t(
              editor === 'tone'
                ? 'tone'
                : editor === 'profile'
                  ? 'voice'
                  : editor === 'alerts'
                    ? 'card'
                    : 'choose'
            )}
          </SheetTitle>
          <SheetDescription className="mb-5 mt-2">
            {t(
              editor === 'tone' || editor === 'profile'
                ? 'voiceHint'
                : editor === 'alerts'
                  ? 'cardHint'
                  : editor === 'facts'
                    ? 'commentsHint'
                    : 'reviewsHint'
            )}
            {(editor === 'five' || editor === 'four') && (
              <span className="mt-2 block">{t('ratingCoupling')}</span>
            )}
          </SheetDescription>
          {editor === 'alerts' && businessId ? (
            <DriftAlertSettings key={businessId} businessId={businessId} />
          ) : editor === 'tone' ? (
            <VoiceToneSection
              key={businessId}
              initial={normalizeStoredTones(business.data?.settings?.voiceTone)}
            />
          ) : editor === 'profile' ? (
            <VoiceProfileSection key={businessId} />
          ) : (
            <div className="space-y-2">
              {(['auto', 'manual', ...(editor === 'four' ? ['skip' as const] : [])] as const).map(
                (mode) => (
                  <ActionButton
                    key={mode}
                    variant="secondary"
                    className="w-full justify-start"
                    disabled={save.isPending || !canEdit}
                    onClick={() => update(mode)}
                  >
                    {t(mode)}
                  </ActionButton>
                )
              )}
            </div>
          )}
        </SheetContent>
      </Sheet>
    </>
  );
}
