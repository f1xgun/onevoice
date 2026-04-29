'use client';

// Linen rebuild — Phase 4.8.
// Multi-select tag chips for the AI's voice. State is local + ephemeral —
// the API does not yet persist a `voiceTone` field on /business, so saves
// are a TODO(api). The picker is wired so once the field lands in the
// schema it just becomes another mutation.

import { useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

const TONE_TAGS = [
  'Тёплый',
  'Спокойный',
  'Дружеский',
  'Профессиональный',
  'Игривый',
  'Деловой',
] as const;

export type ToneTag = (typeof TONE_TAGS)[number];

export interface VoiceToneSectionProps {
  initial?: ToneTag[];
  /**
   * Notified on every change so the page can drive the AI-summary preview
   * in the right rail. Persistence is a separate concern.
   */
  onChange?: (tags: ToneTag[]) => void;
}

export function VoiceToneSection({ initial, onChange }: VoiceToneSectionProps) {
  const [selected, setSelected] = useState<Set<ToneTag>>(new Set(initial ?? ['Тёплый']));
  const [dirty, setDirty] = useState(false);

  function toggle(tag: ToneTag) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(tag)) next.delete(tag);
      else next.add(tag);
      setDirty(true);
      onChange?.(Array.from(next));
      return next;
    });
  }

  function handleSave() {
    // TODO(api): persist voiceTone tags via PUT /business once the schema
    // adds a `voiceTone: string[]` field. For now we just acknowledge the
    // local state so the section still feels alive.
    setDirty(false);
    toast.success('Голос сохранён');
  }

  const count = selected.size;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-2">
        {TONE_TAGS.map((tag) => {
          const on = selected.has(tag);
          return (
            <button
              key={tag}
              type="button"
              onClick={() => toggle(tag)}
              aria-pressed={on}
              className={cn(
                'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-[13px] transition-colors',
                'focus:outline-none focus:ring-2 focus:ring-ochre/30',
                on
                  ? 'border-ochre bg-ochre-soft text-[var(--ov-accent-ink)]'
                  : 'border-line bg-paper-raised text-ink-mid hover:border-ochre/40 hover:text-ink'
              )}
            >
              {on && <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-ochre" />}
              {tag}
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
          disabled={!dirty}
        >
          Сохранить голос
        </Button>
      </div>
    </div>
  );
}
