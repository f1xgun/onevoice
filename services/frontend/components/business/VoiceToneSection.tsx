'use client';

// Multi-select tag chips for the AI's voice/tone. Persists via
// PUT /business/voice-tone (handler: services/api/internal/handler/business.go).
// Stored as stable ids (e.g. "warm") in business.settings.voiceTone —
// display labels live in lib/tones.ts so the DB stays locale-agnostic.
//
// Self-heal: older records may hold Russian labels ("Деловой") instead of
// ids ("businesslike"). normalizeStoredTones() rewrites them on load; the
// next save flushes the canonical-id form to the backend.

import { useEffect, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { api } from '@/lib/api';
import { cn } from '@/lib/utils';
import { TONE_OPTIONS, type ToneId, toneLabel } from '@/lib/tones';

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
  const [selected, setSelected] = useState<Set<ToneId>>(new Set(initial ?? []));
  const [dirty, setDirty] = useState(false);
  const qc = useQueryClient();

  // Sync internal state when the parent's `initial` prop changes — the
  // /business query loads async, so `initial` arrives as [] on first render
  // and updates to the persisted value once data lands.
  const initialKey = (initial ?? []).slice().sort().join('|');
  useEffect(() => {
    if (dirty) return; // user is mid-edit — don't clobber their selection
    setSelected(new Set(initial ?? []));
  }, [initialKey, dirty]); // eslint-disable-line react-hooks/exhaustive-deps

  const mutation = useMutation({
    mutationFn: (ids: ToneId[]) =>
      api.put('/business/voice-tone', { tones: ids }).then((r) => r.data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['business'] });
      setDirty(false);
      toast.success('Голос сохранён');
    },
    onError: () => toast.error('Не получилось сохранить'),
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
          {count === 0
            ? 'Ничего не выбрано — это тоже вариант, OneVoice выберет нейтральный тон.'
            : `Выбрано ${count} — можно больше или ничего.`}
        </p>
        <Button
          type="button"
          variant="primary"
          size="md"
          onClick={handleSave}
          disabled={!dirty || mutation.isPending}
        >
          {mutation.isPending ? 'Сохраняем…' : 'Сохранить голос'}
        </Button>
      </div>
    </div>
  );
}
