'use client';

import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { useQuery } from '@tanstack/react-query';
import { PenLine } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Controller, useForm } from 'react-hook-form';
import { z } from 'zod';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppTextarea as Textarea } from '@/components/design-system/AppInput';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { bizApi } from '@/lib/api/business-api';
import {
  COMPOSE_POST_TYPES,
  buildComposeInstruction,
  isComposePostType,
  type ComposeDestination,
} from '@/lib/compose-instruction';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { channelConnectionState } from '@/lib/constants/integrationStatus';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { usePlatforms, type EnrichedPlatform } from '@/lib/hooks/usePlatforms';
import { useBusinessStore } from '@/lib/stores/business';
import { trackEvent } from '@/lib/telemetry';

interface ComposeIntegration {
  platform: string;
  status?: string;
  metadata?: Record<string, unknown> | null;
}

const composeSchema = z.object({
  postType: z.enum(['announcement', 'promo', 'newArrival']),
  topic: z.string().trim().min(1),
  channels: z.array(z.string()).min(1),
});

type ComposeFormData = z.infer<typeof composeSchema>;

export interface GuidedComposeProps {
  onCompose: (instruction: string) => void;
  disabled?: boolean;
  className?: string;
}

export function confirmedComposeDestinations(
  platforms: readonly EnrichedPlatform[],
  integrations: readonly ComposeIntegration[]
): ComposeDestination[] {
  return platforms
    .filter((platform) => platform.status === 'active' && platform.id !== 'google_business')
    .filter(
      (platform) =>
        channelConnectionState(
          integrations.filter((integration) => integration.platform === platform.id)
        ) === 'connected'
    )
    .map((platform) => ({ id: platform.id, label: platform.fullLabel }));
}

export function GuidedCompose({ onCompose, disabled = false, className }: GuidedComposeProps) {
  const t = useTranslations('gettingStarted.compose');
  const [open, setOpen] = useState(false);
  const topicFieldId = useId();
  const businessId = useBusinessStore((state) => state.activeBusinessId);
  const registry = usePlatforms();
  const integrations = useQuery<ComposeIntegration[]>({
    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(businessId),
    queryFn: () =>
      bizApi(businessId!)
        .get<ComposeIntegration[]>(BIZ_API_PATHS.INTEGRATIONS.ROOT)
        .then((response) => {
          if (!Array.isArray(response.data)) throw new Error('Invalid integration list response');
          return response.data;
        }),
    enabled: !!businessId,
    retry: false,
  });
  const destinations = useMemo(
    () =>
      registry.isSuccess && integrations.isSuccess
        ? confirmedComposeDestinations(registry.platforms, integrations.data)
        : [],
    [integrations.data, integrations.isSuccess, registry.isSuccess, registry.platforms]
  );
  const destinationIDs = useMemo(
    () => new Set(destinations.map((destination) => destination.id)),
    [destinations]
  );
  const initializedBusiness = useRef<string | null>(null);
  const {
    control,
    register,
    handleSubmit,
    reset,
    getValues,
    setValue,
    watch,
    formState: { errors },
  } = useForm<ComposeFormData>({
    resolver: zodResolver(composeSchema),
    defaultValues: { postType: 'announcement', topic: '', channels: [] },
  });
  const selectedChannels = watch('channels');
  const topic = watch('topic');

  useEffect(() => {
    if (initializedBusiness.current !== businessId) {
      initializedBusiness.current = null;
      reset({ postType: 'announcement', topic: '', channels: [] });
    }
    if (!businessId || !registry.isSuccess || !integrations.isSuccess) return;
    if (initializedBusiness.current === null && destinations.length > 0) {
      initializedBusiness.current = businessId;
      setValue(
        'channels',
        destinations.map((destination) => destination.id)
      );
      return;
    }
    const selected = getValues('channels');
    const stillConnected = selected.filter((channel) => destinationIDs.has(channel));
    if (stillConnected.length !== selected.length) {
      setValue('channels', stillConnected, { shouldValidate: true });
    }
  }, [
    businessId,
    destinationIDs,
    destinations,
    getValues,
    integrations.isSuccess,
    registry.isSuccess,
    reset,
    setValue,
  ]);

  const submit = useCallback(
    (values: ComposeFormData) => {
      if (disabled || !businessId || initializedBusiness.current !== businessId) return;
      const selected = destinations.filter((destination) =>
        values.channels.includes(destination.id)
      );
      if (selected.length !== values.channels.length || selected.length === 0) return;
      const composed = buildComposeInstruction(values.postType, values.topic, selected);
      if (composed === null) return;
      trackEvent('activation', 'guided_compose', {
        metadata: { postType: values.postType, platforms: selected.map(({ id }) => id).join(',') },
      });
      onCompose(composed);
      reset({ postType: values.postType, topic: '', channels: destinations.map(({ id }) => id) });
      setOpen(false);
    },
    [businessId, destinations, disabled, onCompose, reset]
  );

  const loading = registry.isPending || integrations.isPending;
  const loadError = registry.isError || integrations.isError;
  const canSubmit =
    !disabled &&
    !!businessId &&
    !loading &&
    !loadError &&
    topic.trim().length > 0 &&
    selectedChannels.length > 0;

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        disabled={disabled}
        className={
          'inline-flex items-center gap-2 rounded-full border border-line bg-paper-raised px-4 py-2 text-sm text-ink-mid transition-colors hover:bg-paper-sunken hover:text-ink disabled:cursor-not-allowed disabled:opacity-50 ' +
          (className ?? '')
        }
      >
        <PenLine size={14} aria-hidden />
        {t('trigger')}
      </button>
    );
  }

  return (
    <form
      aria-label={t('title')}
      onSubmit={handleSubmit(submit)}
      className={
        'w-full max-w-md space-y-4 rounded-md border border-line bg-paper-raised p-4 text-left ' +
        (className ?? '')
      }
    >
      <div className="space-y-1">
        <p className="text-sm font-medium text-ink">{t('title')}</p>
        <p className="text-[13px] text-ink-mid">{t('subtitle')}</p>
      </div>

      <fieldset className="space-y-2">
        <legend className="mb-1 text-xs font-medium text-ink-soft">{t('typeLabel')}</legend>
        <Controller
          control={control}
          name="postType"
          render={({ field }) => (
            <RadioGroup
              value={field.value}
              onValueChange={(next) => {
                if (isComposePostType(next)) field.onChange(next);
              }}
            >
              {COMPOSE_POST_TYPES.map((type) => (
                <label
                  key={type}
                  className="flex cursor-pointer items-center gap-2 text-sm text-ink-mid"
                >
                  <RadioGroupItem value={type} />
                  {t(`types.${type}`)}
                </label>
              ))}
            </RadioGroup>
          )}
        />
      </fieldset>

      <div className="space-y-1.5">
        <Label htmlFor={topicFieldId} className="text-xs font-medium text-ink-soft">
          {t('topicLabel')}
        </Label>
        <Textarea
          id={topicFieldId}
          {...register('topic')}
          placeholder={t('topicPlaceholder')}
          rows={3}
          className="border-line bg-paper text-ink"
        />
      </div>

      <fieldset className="space-y-2">
        <legend className="text-xs font-medium text-ink-soft">{t('channelsLabel')}</legend>
        {loading ? (
          <p className="text-sm text-ink-soft">{t('channelsLoading')}</p>
        ) : loadError ? (
          <p role="alert" className="text-sm text-danger">
            {t('channelsError')}
          </p>
        ) : destinations.length === 0 ? (
          <p className="text-sm text-ink-soft">{t('channelsEmpty')}</p>
        ) : (
          <div className="space-y-2">
            {destinations.map((destination) => (
              <label
                key={destination.id}
                className="flex min-h-11 cursor-pointer items-center gap-2 text-sm text-ink-mid"
              >
                <input
                  type="checkbox"
                  value={destination.id}
                  {...register('channels')}
                  className="size-4 rounded border-control accent-brand"
                />
                {destination.label}
              </label>
            ))}
          </div>
        )}
        {errors.channels && destinations.length > 0 ? (
          <p role="alert" className="text-xs text-danger">
            {t('channelsRequired')}
          </p>
        ) : null}
        <p className="text-xs text-ink-soft">{t('approvalHint')}</p>
      </fieldset>

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={() => setOpen(false)}>
          {t('cancel')}
        </Button>
        <Button type="submit" variant="primary" size="sm" disabled={!canSubmit}>
          {t('submit')}
        </Button>
      </div>
    </form>
  );
}
