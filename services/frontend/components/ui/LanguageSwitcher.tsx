'use client';

// LanguageSwitcher — globe icon button that opens a dropdown of locales.
//
// Why a globe + dropdown (not an inline Select): the globe is the web's
// conventional, space-frugal affordance for language choice — it reads the
// same in the 56px nav-rail footer and the auth-screen corner without a
// visible code label competing for space. The open menu lists each locale
// by its endonym (its own native name) with a check on the active one,
// which is more legible than bare "RU"/"EN" codes.
//
// Flow:
//   1. `useLocale()` (next-intl) reads the current value.
//   2. On select we POST to `/api/locale` so the cookie is persisted
//      server-side and the next render picks it up.
//   3. `router.refresh()` re-runs the request config (`lib/i18n/request.ts`),
//      pulling fresh messages without a full page reload.
//   4. `useTransition` disables the trigger while the refresh is in flight,
//      preventing double-clicks from racing.
//   5. On fetch failure (network error or non-2xx response) we surface a
//      sonner toast and revert the optimistic selection so the UI never
//      silently desyncs from the persisted cookie.
//
// The aria-label is the literal "Language" — universally legible and a
// switcher caption doesn't need translation.

import { useState, useTransition } from 'react';
import { useRouter } from 'next/navigation';
import { useLocale, useTranslations } from 'next-intl';
import { Check, Globe } from 'lucide-react';
import { toast } from 'sonner';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { cn } from '@/lib/utils';
import { LOCALE_LABELS, SUPPORTED_LOCALES, type Locale } from '@/lib/i18n/locales';

type Side = 'top' | 'right' | 'bottom' | 'left';
type Align = 'start' | 'center' | 'end';

export interface LanguageSwitcherProps {
  className?: string;
  /** Which side of the trigger the menu opens on. Defaults to 'bottom'. */
  side?: Side;
  /** Menu alignment against the trigger. Defaults to 'end'. */
  align?: Align;
}

export function LanguageSwitcher({
  className,
  side = 'bottom',
  align = 'end',
}: LanguageSwitcherProps) {
  const locale = useLocale();
  const tCommon = useTranslations('common');
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  // Local override so we can show the picked value optimistically and
  // revert it if /api/locale fails. When undefined, the active locale is
  // the one resolved from next-intl (server source of truth).
  const [pendingValue, setPendingValue] = useState<Locale | undefined>(undefined);
  const active = pendingValue ?? (locale as Locale);

  function handleSelect(next: Locale) {
    if (next === active) return;
    const previous = active;
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
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Language"
          data-testid="language-switcher"
          disabled={isPending}
          className={cn(
            'flex h-10 w-10 items-center justify-center rounded-md text-ink-soft transition-colors hover:bg-paper-sunken hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50',
            className
          )}
        >
          <Globe size={18} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side={side} align={align} className="min-w-[9rem]">
        {SUPPORTED_LOCALES.map((code) => (
          <DropdownMenuItem
            key={code}
            onSelect={() => handleSelect(code)}
            className="justify-between gap-6"
          >
            {LOCALE_LABELS[code]}
            {active === code && <Check className="h-4 w-4 text-ink" aria-hidden />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
