'use client';

import { useTranslations } from 'next-intl';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';
import type { WhitelistMode } from '@/types/project';

interface WhitelistRadioProps {
  value: WhitelistMode;
  onChange: (v: WhitelistMode) => void;
  name?: string;
}

// Stable iteration order — labels + helper copy resolve through
// projects.whitelist.options.<mode>.{label,helper}.
const OPTION_VALUES: readonly WhitelistMode[] = ['inherit', 'all', 'explicit', 'none'];

export function WhitelistRadio({ value, onChange, name }: WhitelistRadioProps) {
  const tWhitelist = useTranslations('projects.whitelist.options');
  return (
    <RadioGroup
      value={value}
      onValueChange={(v) => onChange(v as WhitelistMode)}
      name={name}
      className="space-y-3"
    >
      {OPTION_VALUES.map((mode) => {
        const id = `whitelist-${mode}`;
        return (
          <div
            key={mode}
            className={cn(
              'flex items-start gap-3 rounded-md border p-3 transition-colors',
              value === mode ? 'border-primary bg-accent' : 'border-border'
            )}
          >
            <RadioGroupItem value={mode} id={id} className="mt-0.5" />
            <div className="flex-1">
              <Label htmlFor={id} className="cursor-pointer text-sm font-medium">
                {tWhitelist(`${mode}.label`)}
              </Label>
              <p className="mt-1 text-xs text-muted-foreground">{tWhitelist(`${mode}.helper`)}</p>
            </div>
          </div>
        );
      })}
    </RadioGroup>
  );
}
