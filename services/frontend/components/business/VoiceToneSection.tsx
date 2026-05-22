'use client';

// Multi-select tag chips for the AI's voice/tone. Persists via
// PUT /business/voice-tone (handler: services/api/internal/handler/business.go).
// Stored as stable ids (e.g. "warm") in business.settings.voiceTone —
// display labels live in lib/tones.ts so the DB stays locale-agnostic.
//
// Self-heal: older records may hold Russian labels ("Деловой") instead of
// ids ("businesslike"). normalizeStoredTones() rewrites them on load; the
// next save flushes the canonical-id form to the backend.

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { cn } from '@/lib/utils';
import { createToneLabel, createToneOptions, type ToneId } from '@/lib/tones';

export type { ToneId };

export interface VoiceToneSectionProps {
  initial?: ToneId[];
  /**
   * Notified on every change so the page can drive the AI-summary preview
   * in the right rail. Persistence is a separate concern.
   */
  onChange?: (ids: ToneId[]) => void;
}

export function VoiceToneSection({ initial, onChange }: VoiceToneSectionProps) {
  const tVoice = useTranslations('business.voiceTone');
  const tToneOptions = useTranslations('business.voiceTone.options');
  // Request-scoped tone options/labels (B1). The factory output gets a
  // stable identity per translator-render so consumers that close over
  // `toneLabel` don't tear when the locale switches.
  const TONE_OPTIONS = useMemo(() => createToneOptions(tToneOptions), [tToneOptions]);
  const toneLabel = useMemo(() => createToneLabel(tToneOptions), [tToneOptions]);
  const [selected, setSelected] = useState<Set<ToneId>>(new Set(initial ?? []));
  const [dirty, setDirty] = useState(false);
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const canEdit = usePermission('business.update').allowed;

  // Sync internal state when the parent's `initial` prop changes — the
  // /business query loads async, so `initial` arrives as [] on first render
  // and updates to the persisted value once data lands.
  const initialKey = (initial ?? []).slice().sort().join('|');
  useEffect(() => {
    if (dirty) return; // user is mid-edit — don't clobber their selection
    setSelected(new Set(initial ?? []));
  }, [initialKey, dirty]); // eslint-disable-line react-hooks/exhaustive-deps

  const mutation = useMutation({
    mutationFn: (ids: ToneId[]) => {
      if (!activeBusinessId) return Promise.reject(new Error('No active business'));
      return bizApi(activeBusinessId)
        .put(BIZ_API_PATHS.BUSINESS.VOICE_TONE, { tones: ids })
        .then((r) => r.data);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId) });
      setDirty(false);
      toast.success(tVoice('saved'));
    },
    onError: () => toast.error(tVoice('saveError')),
  });

  function toggle(id: ToneId) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      setDirty(true);
      onChange?.(Array.from(next));
      return next;
    });
  }

  function handleSave() {
    mutation.mutate(Array.from(selected));
  }

  const count = selected.size;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-2">
        {TONE_OPTIONS.map((opt) => {
          const on = selected.has(opt.id);
          return (
            <button
              key={opt.id}
              type="button"
              onClick={() => toggle(opt.id)}
              aria-pressed={on}
              className={cn(
                'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-[13px] transition-colors',
                'focus:ring-ochre/30 focus:outline-none focus:ring-2',
                on
                  ? 'border-ochre bg-ochre-soft text-[var(--ov-accent-ink)]'
                  : 'hover:border-ochre/40 border-line bg-paper-raised text-ink-mid hover:text-ink'
              )}
            >
              {on && <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-ochre" />}
              {toneLabel(opt.id)}
            </button>
          );
        })}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
        <p className="text-xs text-ink-soft">
          {count === 0 ? tVoice('noneHint') : tVoice('selectedHint', { count })}
        </p>
        <Button
          type="button"
          variant="primary"
          size="md"
          onClick={handleSave}
          disabled={!dirty || mutation.isPending || !canEdit}
        >
          {mutation.isPending ? tVoice('saving') : tVoice('saveButton')}
        </Button>
      </div>
    </div>
  );
}
