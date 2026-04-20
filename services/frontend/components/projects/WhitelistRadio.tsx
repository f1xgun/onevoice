'use client';

import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';
import type { WhitelistMode } from '@/types/project';

interface WhitelistRadioProps {
  value: WhitelistMode;
  onChange: (v: WhitelistMode) => void;
  name?: string;
}

interface WhitelistOption {
  value: WhitelistMode;
  label: string;
  helper: string;
}

const OPTIONS: readonly WhitelistOption[] = [
  {
    value: 'inherit',
    label: 'Как у бизнеса',
    helper: 'Использовать настройки бизнеса (по умолчанию все доступные инструменты).',
  },
  {
    value: 'all',
    label: 'Все инструменты',
    helper: 'Любой инструмент активной интеграции доступен LLM.',
  },
  {
    value: 'explicit',
    label: 'Выбранные',
    helper: 'Разрешить только отмеченные ниже.',
  },
  {
    value: 'none',
    label: 'Никаких',
    helper: 'LLM может отвечать, но не будет выполнять действия.',
  },
];

export function WhitelistRadio({ value, onChange, name }: WhitelistRadioProps) {
  return (
    <RadioGroup
      value={value}
      onValueChange={(v) => onChange(v as WhitelistMode)}
      name={name}
      className="space-y-3"
    >
      {OPTIONS.map((opt) => {
        const id = `whitelist-${opt.value}`;
        return (
          <div
            key={opt.value}
            className={cn(
              'flex items-start gap-3 rounded-md border p-3 transition-colors',
              value === opt.value ? 'border-primary bg-accent/40' : 'border-border'
            )}
          >
            <RadioGroupItem value={opt.value} id={id} className="mt-0.5" />
            <div className="flex-1">
              <Label htmlFor={id} className="cursor-pointer text-sm font-medium">
                {opt.label}
              </Label>
              <p className="mt-1 text-xs text-muted-foreground">{opt.helper}</p>
            </div>
          </div>
        );
      })}
    </RadioGroup>
  );
}
