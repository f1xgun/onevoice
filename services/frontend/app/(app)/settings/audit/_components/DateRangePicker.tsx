'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Calendar } from '@/components/ui/calendar';

interface Props {
  from?: string;
  to?: string;
  onChange: (from?: string, to?: string) => void;
}

const MS_PER_SEC = 1000;
const SEC_PER_MIN = 60;
const MIN_PER_HOUR = 60;
const HOURS_PER_DAY = 24;
const DAYS_IN_WEEK = 7;
const DAYS_IN_MONTH = 30;
const TOLERANCE_MIN = 2;
const HOUR_MS = MIN_PER_HOUR * SEC_PER_MIN * MS_PER_SEC;
const PRESET_TOLERANCE_MS = TOLERANCE_MIN * SEC_PER_MIN * MS_PER_SEC;

// Window presets exposed as chips. Hours are wall-clock from "now" — when
// the user clicks "24 часа" we compute (now-24h .. now) and emit ISO
// strings up to the parent. The custom popover sits next to the chips
// and shows a range calendar; selecting both endpoints auto-closes it.
const PRESETS: ReadonlyArray<{ id: '24h' | '7d' | '30d'; hours: number }> = [
  { id: '24h', hours: HOURS_PER_DAY },
  { id: '7d', hours: HOURS_PER_DAY * DAYS_IN_WEEK },
  { id: '30d', hours: HOURS_PER_DAY * DAYS_IN_MONTH },
];

function isPresetSelected(hours: number, from?: string, to?: string): boolean {
  if (!from || !to) return false;
  const f = new Date(from).getTime();
  const t = new Date(to).getTime();
  if (Number.isNaN(f) || Number.isNaN(t)) return false;
  const expected = hours * HOUR_MS;
  return Math.abs(t - f - expected) < PRESET_TOLERANCE_MS;
}

export function DateRangePicker({ from, to, onChange }: Props) {
  const t = useTranslations('audit.filters');
  const [customOpen, setCustomOpen] = useState(false);
  const isCustom = !PRESETS.some((p) => isPresetSelected(p.hours, from, to));

  function applyPreset(hours: number) {
    const now = new Date();
    const start = new Date(now.getTime() - hours * HOUR_MS);
    onChange(start.toISOString(), now.toISOString());
  }

  function labelFor(id: '24h' | '7d' | '30d' | 'custom'): string {
    switch (id) {
      case '24h':
        return t('datePreset24h');
      case '7d':
        return t('datePreset7d');
      case '30d':
        return t('datePreset30d');
      case 'custom':
        return t('datePresetCustom');
    }
  }

  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="text-sm text-ink-soft">{t('dateLabel')}</legend>
      <div className="flex flex-wrap gap-2">
        {PRESETS.map((p) => {
          const active = isPresetSelected(p.hours, from, to);
          return (
            <button
              key={p.id}
              type="button"
              data-testid={`date-preset-${p.id}`}
              aria-pressed={active}
              onClick={() => applyPreset(p.hours)}
              className={
                active
                  ? 'rounded-full bg-ink px-3 py-1 text-sm text-paper'
                  : 'rounded-full border border-line px-3 py-1 text-sm text-ink hover:bg-paper-sunken'
              }
            >
              {labelFor(p.id)}
            </button>
          );
        })}
        <Popover open={customOpen} onOpenChange={setCustomOpen}>
          <PopoverTrigger asChild>
            <button
              type="button"
              data-testid="date-preset-custom"
              aria-pressed={isCustom}
              className={
                isCustom
                  ? 'rounded-full bg-ink px-3 py-1 text-sm text-paper'
                  : 'rounded-full border border-line px-3 py-1 text-sm text-ink hover:bg-paper-sunken'
              }
            >
              {labelFor('custom')}
            </button>
          </PopoverTrigger>
          <PopoverContent className="w-auto p-2">
            <Calendar
              mode="range"
              selected={{
                from: from ? new Date(from) : undefined,
                to: to ? new Date(to) : undefined,
              }}
              onSelect={(range) => {
                onChange(range?.from?.toISOString(), range?.to?.toISOString());
                if (range?.from && range?.to) setCustomOpen(false);
              }}
            />
          </PopoverContent>
        </Popover>
      </div>
    </fieldset>
  );
}
