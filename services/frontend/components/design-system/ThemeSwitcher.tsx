'use client';

import { useTranslations } from 'next-intl';
import { SunMoon } from 'lucide-react';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { THEMES, isTheme } from '@/lib/theme';
import { cn } from '@/lib/utils';
import { useThemeSwitcher } from '@/hooks/useThemeSwitcher';

interface ThemeSwitcherProps {
  className?: string;
  side?: 'top' | 'right' | 'bottom' | 'left';
  align?: 'start' | 'center' | 'end';
}

export function ThemeSwitcher({ className, side = 'bottom', align = 'end' }: ThemeSwitcherProps) {
  const t = useTranslations('theme');
  const { active, disabled, select } = useThemeSwitcher();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={t('label')}
          disabled={disabled}
          className={cn(
            'flex min-h-11 min-w-11 items-center justify-center rounded-md text-ink-soft transition-colors hover:bg-paper-sunken hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:bg-paper-sunken disabled:text-ink-soft motion-reduce:transition-none',
            className
          )}
        >
          <SunMoon size={18} aria-hidden />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side={side} align={align} className="min-w-[9rem]">
        <DropdownMenuRadioGroup
          value={active}
          onValueChange={(value) => {
            if (isTheme(value)) void select(value);
          }}
        >
          {THEMES.map(({ value, label }) => (
            <DropdownMenuRadioItem
              key={value}
              value={value}
              disabled={disabled}
              className="min-h-11 text-meta"
            >
              {t(label)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
