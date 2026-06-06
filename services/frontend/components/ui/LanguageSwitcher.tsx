'use client';

// LanguageSwitcher — compact UI to flip the current locale.
//
// Why a Select (not a ToggleGroup): only two options today, but the
// architecture is meant to extend to more locales without UI rework.
// `Select` ships in this project already (`components/ui/select.tsx`);
// adding a new shadcn primitive just for this would violate AGENTS.md
// guidance on minimizing shadcn imports.
//
// Flow:
//   1. `useLocale()` (next-intl) reads the current value.
//   2. On change we POST to `/api/locale` so the cookie is persisted
//      server-side and the next render picks it up.
//   3. `router.refresh()` re-runs the request config (`lib/i18n/request.ts`),
//      pulling fresh messages without a full page reload.
//   4. `useTransition` keeps the trigger interactive but disabled while
//      the refresh is in flight, preventing double-clicks from racing.
//   5. On fetch failure (network error or non-2xx response) we surface
//      a sonner toast and revert the optimistic selection so the UI
//      never silently desyncs from the persisted cookie.
//
// The aria-label is the literal "Language" — a language code identifier
// is universally legible and a switcher caption doesn't need translation.

import { useState, useTransition } from 'react';
import { useRouter } from 'next/navigation';
import { useLocale, useTranslations } from 'next-intl';
import { toast } from 'sonner';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { cn } from '@/lib/utils';
import { isLocale, SUPPORTED_LOCALES, type Locale } from '@/lib/i18n/locales';

export interface LanguageSwitcherProps {
  className?: string;
}

export function LanguageSwitcher({ className }: LanguageSwitcherProps) {
  const locale = useLocale();
  const tCommon = useTranslations('common');
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const [pendingValue, setPendingValue] = useState<Locale | undefined>(undefined);

  function handleChange(next: string) {
    if (!isLocale(next) || next === locale) return;
    const previous = locale as Locale;
    setPendingValue(next);
    startTransition(async () => {
      try {
        const res = await fetch('/api/locale', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ locale: next }),
        });
        if (!res.ok) {
          toast.error(tCommon('toasts.languageSwitchFailed'));
          setPendingValue(previous);
          return;
        }
        setPendingValue(undefined);
        router.refresh();
      } catch {
        toast.error(tCommon('toasts.languageSwitchFailed'));
        setPendingValue(previous);
      }
    });
  }

  return (
    <Select value={pendingValue ?? locale} onValueChange={handleChange} disabled={isPending}>
      <SelectTrigger
        aria-label="Language"
        data-testid="language-switcher"
        className={cn('h-8 w-[64px] px-2 text-xs uppercase', className)}
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {SUPPORTED_LOCALES.map((code) => (
          <SelectItem key={code} value={code} className="text-xs uppercase">
            {code}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
