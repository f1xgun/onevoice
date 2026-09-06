'use client';

// components/onboarding/SectionHelp.tsx — reusable, dismissible per-section
// primer. On first run it renders an info callout (mirrors the
// WhitelistWarningBanner dismissible-callout pattern); once dismissed it
// collapses to a compact "?" Popover trigger so the copy stays reachable for
// re-reading. Dismiss state is per-device / per-organization localStorage —
// no user-pref backend, consistent with existing hints.

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { HelpCircle, Info, X } from 'lucide-react';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { useBusinessStore } from '@/lib/stores/business';
import { readDismissed, sectionHelpDismissKey, writeDismissed } from '@/lib/onboarding/dismiss';
import { cn } from '@/lib/utils';

export type SectionHelpKey = 'chat' | 'integrations' | 'business';

export interface SectionHelpProps {
  section: SectionHelpKey;
  className?: string;
}

export function SectionHelp({ section, className }: SectionHelpProps) {
  const t = useTranslations('help');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const dismissKey = sectionHelpDismissKey(section, activeBusinessId);
  const [dismissed, setDismissed] = useState<boolean>(() => readDismissed(dismissKey));

  useEffect(() => {
    setDismissed(readDismissed(dismissKey));
  }, [dismissKey]);

  const title = t(`${section}.title`);
  const body = t(`${section}.body`);

  function handleDismiss() {
    writeDismissed(dismissKey);
    setDismissed(true);
  }

  if (dismissed) {
    return (
      <Popover>
        <PopoverTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            aria-label={t('reopenAria', { title })}
            className={cn('text-ink-soft', className)}
          >
            <HelpCircle className="h-4 w-4" />
            {t('reopen')}
          </Button>
        </PopoverTrigger>
        <PopoverContent align="start" className="text-sm">
          <p className="font-medium text-ink">{title}</p>
          <p className="mt-1 text-ink-mid">{body}</p>
        </PopoverContent>
      </Popover>
    );
  }

  return (
    <div
      className={cn(
        'rounded-md border border-line bg-paper-sunken p-4 text-sm text-ink-mid',
        className
      )}
    >
      <div className="flex gap-3">
        <Info className="mt-0.5 h-4 w-4 shrink-0 text-[var(--ov-info)]" />
        <div className="min-w-0 flex-1">
          <p className="font-medium text-ink">{title}</p>
          <p className="mt-1">{body}</p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={handleDismiss}
          aria-label={t('dismissAria')}
          className="shrink-0"
        >
          <X className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
