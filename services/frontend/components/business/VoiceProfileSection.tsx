'use client';

// Free-form brand-voice profile editor. Reads via GET and persists via
// PUT /businesses/{id}/voice-profile (handler: services/api/internal/handler/business.go).
// Stored in business.settings.voiceProfile as authored prose; the 5A backend
// threads it into the chat loop and review drafter. Unlike voiceTone (the tag
// picker) it is not fanned out to any platform — it changes prompt text only.

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { cn } from '@/lib/utils';

export const VOICE_PROFILE_MAX_LENGTH = 400;

interface VoiceProfileResponse {
  voiceProfile: string;
}

export function VoiceProfileSection() {
  const t = useTranslations('business.voiceProfile');
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const canEdit = usePermission('business.update').allowed;

  const { data, isLoading, isError } = useQuery<string>({
    queryKey: QUERY_KEYS.BUSINESS_VOICE_PROFILE(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<VoiceProfileResponse>(BIZ_API_PATHS.BUSINESS.VOICE_PROFILE)
        .then((r) => r.data.voiceProfile ?? ''),
    enabled: !!activeBusinessId,
    retry: false,
  });

  const [value, setValue] = useState('');
  const [dirty, setDirty] = useState(false);

  const persisted = data ?? '';
  useEffect(() => {
    if (dirty) return;
    setValue(persisted);
  }, [persisted, dirty]);

  const mutation = useMutation({
    mutationFn: (profile: string) => {
      if (!activeBusinessId) return Promise.reject(new Error('No active business'));
      return bizApi(activeBusinessId)
        .put(BIZ_API_PATHS.BUSINESS.VOICE_PROFILE, { voiceProfile: profile })
        .then((r) => r.data);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_VOICE_PROFILE(activeBusinessId) });
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId) });
      setDirty(false);
      toast.success(t('saved'));
    },
    onError: () => toast.error(t('saveError')),
  });

  const count = value.length;
  const overCap = count > VOICE_PROFILE_MAX_LENGTH;

  function handleChange(next: string) {
    setValue(next);
    setDirty(true);
  }

  function handleSave() {
    if (overCap) return;
    mutation.mutate(value);
  }

  return (
    <div className="flex flex-col gap-4">
      <Textarea
        value={value}
        onChange={(e) => handleChange(e.target.value)}
        rows={5}
        maxLength={VOICE_PROFILE_MAX_LENGTH}
        disabled={isLoading || !canEdit}
        placeholder={t('placeholder')}
        aria-label={t('label')}
        aria-invalid={overCap}
      />

      {isError && <p className="text-xs text-[var(--ov-danger)]">{t('loadError')}</p>}

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
          type="button"
          variant="primary"
          size="md"
          onClick={handleSave}
          disabled={!dirty || overCap || mutation.isPending || isLoading || !canEdit}
        >
          {mutation.isPending ? t('saving') : t('saveButton')}
        </Button>
      </div>
    </div>
  );
}
