'use client';

import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';

import { useEffect, useRef } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput, AppTextarea as Textarea } from '@/components/design-system/AppInput';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { Skeleton } from '@/components/ui/skeleton';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { usePermission } from '@/lib/hooks/usePermission';
import { useBusinessStore } from '@/lib/stores/business';
import { cn } from '@/lib/utils';
import { useBusinessVoice, type BusinessVoice } from '@/lib/hooks/useBusinessVoice';

export const VOICE_PROFILE_MAX_LENGTH = 400;
export const REPLY_SIGNATURE_MAX_LENGTH = 200;

const voiceProfileFormSchema = z.object({
  voiceProfile: z.string().refine((value) => Array.from(value).length <= VOICE_PROFILE_MAX_LENGTH),
  replySignature: z
    .string()
    .refine((value) => Array.from(value).length <= REPLY_SIGNATURE_MAX_LENGTH),
});

type VoiceProfileFormValues = z.infer<typeof voiceProfileFormSchema>;
interface VoiceProfileMutationVariables {
  businessId: string;
  profile: string;
  signature: string;
}

export function VoiceProfileSection() {
  const t = useTranslations('business.voiceProfile');
  const tCommon = useTranslations('common');
  const activeBusinessId = useBusinessStore((state) => state.activeBusinessId);
  const readPerm = usePermission('business.read');
  const editPerm = usePermission('business.update');

  const profileQuery = useBusinessVoice(activeBusinessId, readPerm.allowed);

  if (readPerm.isError) return <PermissionLoadError onRetry={readPerm.refetch} />;
  if (!activeBusinessId || (!readPerm.allowed && !readPerm.isLoading)) return null;

  if (readPerm.isLoading || profileQuery.isPending) {
    return (
      <LoadingPlaceholder
        role="status"
        aria-label={tCommon('loading')}
        className="flex flex-col gap-3"
      >
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-8 w-36 self-end" />
      </LoadingPlaceholder>
    );
  }

  if (profileQuery.isError) {
    return (
      <div className="flex flex-col items-start gap-3">
        <p role="alert" className="text-xs text-[var(--ov-danger)]">
          {t('loadError')}
        </p>
        <Button type="button" variant="secondary" size="sm" onClick={() => profileQuery.refetch()}>
          {tCommon('retry')}
        </Button>
      </div>
    );
  }

  if (!profileQuery.isSuccess) return null;

  return (
    <VoiceProfileForm
      key={activeBusinessId}
      businessId={activeBusinessId}
      persistedProfile={profileQuery.data}
      canEdit={editPerm.allowed && !editPerm.isLoading && !editPerm.isError}
      editPermissionError={editPerm.isError}
      retryEditPermission={editPerm.refetch}
    />
  );
}

interface VoiceProfileFormProps {
  businessId: string;
  persistedProfile: BusinessVoice;
  canEdit: boolean;
  editPermissionError: boolean;
  retryEditPermission: () => unknown;
}

function VoiceProfileForm({
  businessId,
  persistedProfile,
  canEdit,
  editPermissionError,
  retryEditPermission,
}: VoiceProfileFormProps) {
  const t = useTranslations('business.voiceProfile');
  const qc = useQueryClient();
  const mountedRef = useRef(false);
  const form = useForm<VoiceProfileFormValues>({
    resolver: zodResolver(voiceProfileFormSchema),
    defaultValues: persistedProfile,
    mode: 'onChange',
  });
  const isDirty = form.formState.isDirty;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    if (!isDirty) form.reset(persistedProfile);
  }, [form, isDirty, persistedProfile]);

  const mutation = useMutation({
    mutationFn: ({
      businessId: requestBusinessId,
      profile,
      signature,
    }: VoiceProfileMutationVariables) =>
      bizApi(requestBusinessId)
        .put(BIZ_API_PATHS.BUSINESS.VOICE_PROFILE, {
          voiceProfile: profile,
          replySignature: signature,
        })
        .then((response) => response.data),
    onSuccess: (_data, variables) => {
      qc.setQueryData(QUERY_KEYS.BUSINESS_VOICE_PROFILE(variables.businessId), {
        voiceProfile: variables.profile,
        replySignature: variables.signature,
      });
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_VOICE_PROFILE(variables.businessId) });
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE(variables.businessId) });
      if (
        !mountedRef.current ||
        form.getValues('voiceProfile') !== variables.profile ||
        form.getValues('replySignature') !== variables.signature
      )
        return;
      form.reset({ voiceProfile: variables.profile, replySignature: variables.signature });
      toast.success(t('saved'));
    },
    onError: () => {
      if (mountedRef.current) toast.error(t('saveError'));
    },
  });

  const value = form.watch('voiceProfile');
  const count = Array.from(value).length;
  const overCap = count > VOICE_PROFILE_MAX_LENGTH;
  const handleSave = form.handleSubmit(({ voiceProfile, replySignature }) => {
    if (!canEdit || mutation.isPending) return;
    mutation.mutate({ businessId, profile: voiceProfile, signature: replySignature });
  });

  return (
    <form className="flex flex-col gap-4" onSubmit={handleSave}>
      <Textarea
        {...form.register('voiceProfile')}
        rows={5}
        disabled={!canEdit}
        placeholder={t('placeholder')}
        aria-label={t('label')}
        aria-invalid={overCap}
      />
      <label className="space-y-2 text-sm text-ink">
        <span className="block">{t('signatureLabel')}</span>
        <AppInput
          {...form.register('replySignature')}
          disabled={!canEdit}
          placeholder={t('signaturePlaceholder')}
          aria-invalid={!!form.formState.errors.replySignature}
        />
        <span className="block text-xs text-ink-soft">
          {t('signatureHint', { max: REPLY_SIGNATURE_MAX_LENGTH })}
        </span>
      </label>
      {editPermissionError && <PermissionLoadError onRetry={retryEditPermission} />}
      <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
        <div className="flex flex-col gap-0.5">
          <p className="text-xs text-ink-soft">{t('hint')}</p>
          <p
            className={cn(
              'text-xs tabular-nums',
              overCap ? 'text-[var(--ov-danger)]' : 'text-ink-soft'
            )}
          >
            {t('counter', { count, max: VOICE_PROFILE_MAX_LENGTH })}
          </p>
        </div>
        <Button
          type="submit"
          variant="primary"
          size="md"
          disabled={!isDirty || !form.formState.isValid || mutation.isPending || !canEdit}
        >
          {mutation.isPending ? t('saving') : t('saveButton')}
        </Button>
      </div>
    </form>
  );
}
