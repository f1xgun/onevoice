'use client';

import { useTranslations } from 'next-intl';

import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { cn } from '@/lib/utils';

import { ThemeSwitcher } from './ThemeSwitcher';

interface AppearanceControlsProps {
  className?: string;
  variant?: 'compact' | 'navigation';
}

export function AppearanceControls({ className, variant = 'compact' }: AppearanceControlsProps) {
  const tNav = useTranslations('nav');

  if (variant === 'navigation') {
    return (
      <div data-ov-motion className={cn('grid w-full gap-1', className)}>
        <ThemeSwitcher
          className="w-full justify-start gap-3 px-3 text-meta [&>svg]:shrink-0"
          showLabel
          side="bottom"
          align="start"
        />
        <div className="grid w-full">
          <LanguageSwitcher
            className="col-start-1 row-start-1 h-11 w-full justify-start px-3 motion-reduce:transition-none"
            side="bottom"
            align="start"
          />
          <span
            aria-hidden
            className="pointer-events-none col-start-1 row-start-1 flex items-center pl-[42px] text-meta text-ink-soft"
          >
            {tNav('language')}
          </span>
        </div>
      </div>
    );
  }

  return (
    <div data-ov-motion className={cn('flex shrink-0 items-center gap-1', className)}>
      <ThemeSwitcher className="h-11 w-11 shrink-0" />
      <LanguageSwitcher className="h-11 w-11 shrink-0 motion-reduce:transition-none" />
    </div>
  );
}
