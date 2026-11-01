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
import { VoiceToneSection } from '@/components/business/VoiceToneSection';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { bizApi } from '@/lib/api/business-api';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { useTools } from '@/lib/hooks/useTools';
import { useBusinessToolApprovals } from '@/lib/hooks/useBusinessToolApprovals';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { normalizeStoredTones } from '@/lib/tones';
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
type Editor = 'five' | 'four' | 'facts' | 'tone' | 'profile' | null;

export default function RulesPage() {
  const t = useTranslations('rulesV2');
  const tCatalog = useTranslations('agentTasks.displayName.tools');
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
  function row(title: string, value: string, onClick?: () => void) {
    return (
      <button
        type="button"
        disabled={!onClick}
        aria-label={`${title}: ${value}`}
        onClick={onClick}
        className="flex min-h-20 w-full items-center justify-between gap-4 p-4 text-left disabled:cursor-default"
      >
        <span>
          <span className="block text-base text-ink">{title}</span>
          <span className="mt-1 block text-sm text-ink-mid">{value}</span>
        </span>
        {onClick && <ChevronRight className="h-4 w-4 shrink-0 text-ink-mid" aria-hidden />}
      </button>
    );
  }
  function toolRows(kind: 'comments' | 'posts' | 'card') {
    if (tools.isLoading || approvals.isLoading)
      return <p className="p-4 text-sm text-ink-mid">{t('loading')}</p>;
    if (tools.isError || approvals.isError)
      return (
        <p role="alert" className="p-4 text-sm text-danger">
          {t('loadError')}
        </p>
      );
    const selected = (tools.data ?? []).filter((tool) =>
      kind === 'comments'
        ? /reply.*comment/.test(tool.name)
        : kind === 'posts'
          ? /send_channel_post|create_post|publish_post/.test(tool.name)
          : /update.*(profile|business|info)/.test(tool.name)
    );
    if (!selected.length) return <p className="p-4 text-sm text-ink-mid">{t('none')}</p>;
    return selected.map((tool) => (
      <Link
        key={tool.name}
        href="/settings/tools"
        className="flex min-h-20 items-center justify-between gap-4 p-4"
      >
        <span>
          <span className="block text-ink">
            {tCatalog.has(`${tool.name.replace('__', '.')}.name`)
              ? tCatalog(`${tool.name.replace('__', '.')}.name`)
              : tool.platform}
          </span>
          <span className="mt-1 block text-sm text-ink-mid">
            {t(
              tool.floor === 'forbidden'
                ? 'forbidden'
                : tool.floor === 'auto' && approvals.data?.[tool.name] !== 'manual'
                  ? 'auto'
                  : 'manual'
            )}
          </span>
        </span>
        <ChevronRight className="h-4 w-4 shrink-0" aria-hidden />
      </Link>
    ));
  }
  return (
    <>
      <header className="mx-auto max-w-5xl px-gutter pb-group pt-group md:px-gutter-desktop">
        <h1 className="text-page-title">{t('title')}</h1>
        <p className="mt-1 text-ink-mid">{t('sub')}</p>
      </header>
      <div className="mx-auto grid max-w-5xl gap-group px-gutter pb-group md:px-gutter-desktop lg:grid-cols-2">
        <div className="space-y-group">
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
          {(['comments', 'posts', 'card'] as const).map((kind) => (
            <section key={kind}>
              <h2 className="font-semibold text-ink">{t(kind)}</h2>
              <p className="mb-3 mt-1 text-sm text-ink-mid">{t(`${kind}Hint`)}</p>
              <div className="overflow-hidden rounded-block bg-card">
                {kind === 'comments' &&
                  row(
                    t('facts'),
                    config ? t(config.autoAnswerFacts ? 'auto' : 'manual') : t('unknown'),
                    config && canEdit ? () => setEditor('facts') : undefined
                  )}
                {toolRows(kind)}
              </div>
            </section>
          ))}
        </div>
        <section>
          <h2 className="font-semibold text-ink">{t('voice')}</h2>
          <p className="mb-3 mt-1 text-sm text-ink-mid">{t('voiceHint')}</p>
          <div className="overflow-hidden rounded-block bg-card">
            {row(
              t('tone'),
              normalizeStoredTones(business.data?.settings?.voiceTone)
                .map((tone) => toneLabels(tone))
                .join(', ') || t('noTone'),
              business.isSuccess ? () => setEditor('tone') : undefined
            )}
            {row(
              t('profile'),
              typeof business.data?.settings?.voiceProfile === 'string'
                ? business.data.settings.voiceProfile || t('profile')
                : t('profile'),
              () => setEditor('profile')
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
            {t(editor === 'tone' ? 'tone' : editor === 'profile' ? 'profile' : 'choose')}
          </SheetTitle>
          <SheetDescription className="mb-5 mt-2">
            {t(
              editor === 'tone' || editor === 'profile'
                ? 'voiceHint'
                : editor === 'facts'
                  ? 'commentsHint'
                  : 'reviewsHint'
            )}
            {(editor === 'five' || editor === 'four') && (
              <span className="mt-2 block">{t('ratingCoupling')}</span>
            )}
          </SheetDescription>
          {editor === 'tone' ? (
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
