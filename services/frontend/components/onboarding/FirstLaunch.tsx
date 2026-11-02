'use client';
import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { PointStep } from './PointStep';
import { ActionButton } from '@/components/design-system/ActionButton';
import { AppInput } from '@/components/design-system/AppInput';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';
import { TelegramConnectModal } from '@/components/integrations/TelegramConnectModal';
import { VKCommunityModal } from '@/components/integrations/VKCommunityModal';
import { YandexBusinessConnectModal } from '@/components/integrations/YandexBusinessConnectModal';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { usePermission } from '@/lib/hooks/usePermission';
import { useBusinessVoice } from '@/lib/hooks/useBusinessVoice';
import { useBusinessStore } from '@/lib/stores/business';
import { bizApi } from '@/lib/api/business-api';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import type { Review } from '@/types/review';
import { usePlatformFullLabels } from '@/lib/platforms';

const SIGNATURE_MAX_LENGTH = 200;
const toneSchema = z.object({
  tone: z.enum(['warm', 'businesslike']),
  signature: z.string().max(SIGNATURE_MAX_LENGTH),
});
type ToneValues = z.infer<typeof toneSchema>;
const platforms = ['yandex_business', 'telegram', 'vk'] as const;
type Platform = (typeof platforms)[number];
export function FirstLaunch() {
  const t = useTranslations('launchV2');
  const router = useRouter();
  const businessId = useBusinessStore((state) => state.activeBusinessId);
  const [step, setStep] = useState(1);
  useEffect(() => {
    function readStep() {
      const query = new URLSearchParams(window.location.search);
      const requested = query.get('step');
      setStep(
        query.get('businessId') === businessId && (requested === '2' || requested === '3')
          ? Number(requested)
          : 1
      );
    }
    readStep();
    window.addEventListener('popstate', readStep);
    return () => window.removeEventListener('popstate', readStep);
  }, [businessId]);
  function goToStep(next: number, id = businessId) {
    if (!id) return;
    const path = window.location.pathname === '/onboarding' ? '/onboarding' : '/business/new';
    window.history.replaceState(
      null,
      '',
      `${path}?step=${next}&businessId=${encodeURIComponent(id)}`
    );
    setStep(next);
  }
  return (
    <div className="mx-auto w-full max-w-2xl px-gutter pb-group md:px-gutter-desktop">
      <p className="pt-group text-sm text-ink-mid">{t('step', { step })}</p>
      <div className="mt-2 flex gap-1" aria-hidden>
        {[1, 2, 3].map((part) => (
          <span
            key={part}
            className={`h-1 flex-1 rounded-full ${part <= step ? 'bg-brand' : 'bg-line'}`}
          />
        ))}
      </div>
      {step === 1 ? (
        <PointStep onCreated={(id) => goToStep(2, id)} />
      ) : step === 2 ? (
        <PlatformStep onNext={() => goToStep(3)} />
      ) : (
        <ToneStep onDone={() => router.push('/')} />
      )}
      {step === 3 && (
        <ActionButton variant="ghost" className="mt-3" onClick={() => goToStep(2)}>
          {t('back')}
        </ActionButton>
      )}
    </div>
  );
}
interface StepProps {
  onNext: () => void;
}
function PlatformStep({ onNext }: StepProps) {
  const t = useTranslations('launchV2');
  const labels = usePlatformFullLabels();
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const permission = usePermission('integrations.connect');
  const [connecting, setConnecting] = useState<Platform | null>(null);
  const integrations = useQuery({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(businessId),
    queryFn: () =>
      bizApi(businessId!)
        .get<{ platform: string; status: string }[]>('/integrations')
        .then((r) => r.data),
    enabled: !!businessId,
  });
  function close() {
    setConnecting(null);
    void integrations.refetch();
  }
  return (
    <section className="space-y-group pt-group">
      <div>
        <h1 className="text-2xl font-semibold text-ink">{t('platformTitle')}</h1>
        <p className="mt-1 text-ink-mid">{t('platformHint')}</p>
      </div>
      {integrations.isPending ? (
        <p role="status">{t('step', { step: 2 })}</p>
      ) : integrations.isError ? (
        <div role="alert">
          <p>{t('loadError')}</p>
          <ActionButton onClick={() => integrations.refetch()}>{t('retry')}</ActionButton>
        </div>
      ) : (
        <div className="rounded-block bg-card p-4">
          {platforms.map((platform) => {
            const connected = integrations.data.some(
              (i) => i.platform === platform && i.status === 'active'
            );
            return (
              <div key={platform} className="flex items-start gap-3 py-3">
                <PlatformIcon platform={platform} className="h-8 w-8 shrink-0" />
                <div className="min-w-0 flex-1">
                  <h2 className="font-semibold">{labels[platform]}</h2>
                  <p className="mt-1 text-sm text-ink-mid">
                    {t(connected ? 'connected' : 'unconnected')}
                  </p>
                  {!connected && (
                    <ActionButton
                      variant="secondary"
                      className="mt-3"
                      disabled={!permission.allowed || permission.isLoading || permission.isError}
                      onClick={() => {
                        if (platform === 'vk' && businessId)
                          sessionStorage.setItem('onevoice:launch-vk-business', businessId);
                        setConnecting(platform);
                      }}
                    >
                      {t('connect')}
                    </ActionButton>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
      {permission.isError && <PermissionLoadError onRetry={permission.refetch} />}
      <ActionButton className="w-full" onClick={onNext}>
        {t('next')}
      </ActionButton>
      <ActionButton variant="secondary" className="w-full" onClick={onNext}>
        {t('skip')}
      </ActionButton>
      {connecting === 'telegram' && <TelegramConnectModal open onClose={close} />}
      {connecting === 'vk' && <VKCommunityModal open onClose={close} />}
      {connecting === 'yandex_business' && <YandexBusinessConnectModal open onClose={close} />}
    </section>
  );
}
interface ToneStepProps {
  onDone: () => void;
}
function ToneStep({ onDone }: ToneStepProps) {
  const t = useTranslations('launchV2');
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const permission = usePermission('business.update');
  const voice = useBusinessVoice(businessId);
  const qc = useQueryClient();
  const reviews = useQuery({
    queryKey: ['business', businessId, 'launchReviews'],
    queryFn: () =>
      bizApi(businessId!)
        .get('/reviews')
        .then((response) => {
          const data: unknown = response.data;
          if (Array.isArray(data)) return data as Review[];
          const list = (data as { reviews?: Review[] } | null)?.reviews;
          if (!Array.isArray(list)) throw new Error('Invalid reviews response');
          return list;
        }),
    enabled: !!businessId,
  });
  const realReview = reviews.data?.find(
    (review) =>
      review.platform === 'yandex_business' &&
      review.text &&
      (review.draftReply || review.replyText)
  );
  const form = useForm<ToneValues>({
    resolver: zodResolver(toneSchema),
    defaultValues: { tone: 'warm', signature: '' },
  });
  const save = useMutation({
    mutationFn: async ({ id, values }: { id: string; values: ToneValues }) => {
      await bizApi(id).put('/voice-tone', { tones: [values.tone] });
      await bizApi(id).put('/voice-profile', { replySignature: values.signature.trim() });
    },
    onSuccess: (_result, variables) => {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE(variables.id) });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_VOICE_PROFILE(variables.id) });
      if (sessionStorage.getItem('onevoice:launch-vk-business') === variables.id)
        sessionStorage.removeItem('onevoice:launch-vk-business');
      onDone();
    },
  });
  const signatureDirty = !!form.formState.dirtyFields.signature;
  useEffect(() => {
    if (voice.data && !signatureDirty) form.setValue('signature', voice.data.replySignature);
  }, [voice.data, signatureDirty, form]);
  return (
    <form
      onSubmit={form.handleSubmit((values) => {
        if (businessId && permission.allowed) save.mutate({ id: businessId, values });
      })}
      className="space-y-group pt-group"
    >
      <div>
        <h1 className="text-2xl font-semibold text-ink">{t('toneTitle')}</h1>
        <p className="mt-1 text-ink-mid">{t('toneHint')}</p>
      </div>
      <div className="rounded-block bg-card p-4">
        <p className="text-sm text-ink-mid">{t(realReview ? 'realReview' : 'exampleLabel')}</p>
        <p className="mt-2 text-ink">
          {realReview ? `${realReview.authorName}: ${realReview.text}` : t('example')}
        </p>
        {realReview && (
          <p className="mt-3 text-ink-mid">{realReview.draftReply || realReview.replyText}</p>
        )}
      </div>
      <div className="space-y-4 rounded-block bg-card p-4">
        {(['warm', 'businesslike'] as const).map((tone) => (
          <label key={tone} className="flex cursor-pointer items-start gap-3">
            <input
              type="radio"
              value={tone}
              {...form.register('tone')}
              className="mt-1 h-5 w-5 accent-brand"
            />
            <span>
              <span className="block text-ink">
                {t(
                  tone === 'warm'
                    ? realReview
                      ? 'warm'
                      : 'warmReply'
                    : realReview
                      ? 'short'
                      : 'shortReply'
                )}
              </span>
              <span className="mt-1 block text-sm text-ink-mid">
                {t(tone === 'warm' ? 'warm' : 'short')}
              </span>
            </span>
          </label>
        ))}
      </div>
      <label className="block text-sm text-ink-mid">
        {t('signature')}
        <AppInput
          {...form.register('signature')}
          placeholder={t('signatureHint')}
          className="mt-2"
          aria-invalid={!!form.formState.errors.signature}
        />
      </label>
      {permission.isError && <PermissionLoadError onRetry={permission.refetch} />}
      {!permission.isLoading && !permission.isError && !permission.allowed && (
        <p role="status">{t('readOnly')}</p>
      )}
      {voice.isError && (
        <div role="alert">
          <p>{t('loadError')}</p>
          <ActionButton type="button" variant="secondary" onClick={() => voice.refetch()}>
            {t('retry')}
          </ActionButton>
        </div>
      )}
      {save.isError && (
        <p role="alert" className="text-danger">
          {t('error')}
        </p>
      )}
      <ActionButton
        type="submit"
        className="w-full"
        disabled={save.isPending || !voice.isSuccess || !permission.allowed || permission.isError}
      >
        {t('finish')}
      </ActionButton>
      <p className="text-sm text-ink-mid">{t('savedHint')}</p>
    </form>
  );
}
