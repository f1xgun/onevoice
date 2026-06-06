// components/states/NoConnection.tsx — OneVoice (Linen) offline state
//
// Full-screen "не получается дотянуться" frame. Mirrors the
// "Полноэкранная: нет связи" panel from
// design_handoff_onevoice 2/mocks/mock-states.jsx (ErrorStatesPage):
// calm paper background, big graphite headline, one-line sub, optional
// status-page link, and a mono error code at the bottom.
//
// Render this when the API/orchestrator is provably unreachable — not
// for transient 5xx that one retry would fix. The default is to call
// `window.location.reload()` on retry; pass `onRetry` to override.
// The "Открыть статус" button only renders when `statusUrl` is
// provided — there is no public OneVoice status page yet, so by default
// we just offer the retry.

'use client';

import * as React from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';
import type { Locale } from '@/lib/i18n/locales';
import { cn } from '@/lib/utils';

// BCP-47 tags for `Intl.DateTimeFormat`. Mirrors the table in
// `app/(app)/reviews/page.tsx` — kept inline rather than centralised
// because the only two consumers want different format options anyway.
const INTL_LOCALE_TAG: Record<Locale, string> = {
  ru: 'ru-RU',
  en: 'en-US',
};

export interface NoConnectionProps {
  /** Override the default "reload the page" behavior. */
  onRetry?: () => void;
  /**
   * Where the "Открыть статус" button points. When omitted, the button
   * is not rendered.
   */
  statusUrl?: string;
  /**
   * Optional machine-readable error code. Rendered in mono at the
   * bottom of the panel — e.g. `NET_TIMEOUT_5xx`.
   */
  code?: string;
  /** Optional timestamp string. Defaults to the local time on render. */
  timestamp?: string;
  /** Set to `true` to render full-viewport. Defaults to in-flow card. */
  fullscreen?: boolean;
  className?: string;
}

function formatTime(d: Date, locale: Locale) {
  return d.toLocaleTimeString(INTL_LOCALE_TAG[locale], {
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function NoConnection({
  onRetry,
  statusUrl,
  code = 'NET_UNREACHABLE',
  timestamp,
  fullscreen,
  className,
}: NoConnectionProps) {
  const t = useTranslations('noConnection');
  const locale = useLocale() as Locale;
  const handleRetry = React.useCallback(() => {
    if (onRetry) {
      onRetry();
      return;
    }
    if (typeof window !== 'undefined') {
      window.location.reload();
    }
  }, [onRetry]);

  const [now, setNow] = React.useState<string>(timestamp ?? '');
  React.useEffect(() => {
    if (!timestamp) setNow(formatTime(new Date(), locale));
  }, [timestamp, locale]);

  return (
    <div
      role="alert"
      className={cn(
        'flex w-full items-center justify-center bg-paper',
        fullscreen ? 'min-h-screen p-6' : 'px-6 py-16',
        className
      )}
    >
      <div className="flex w-full max-w-[480px] flex-col items-center gap-4 rounded-lg border border-line bg-paper-raised px-8 py-16 text-center shadow-ov-1">
        <span
          aria-hidden="true"
          className="inline-flex h-14 w-14 items-center justify-center rounded-full border border-[var(--ov-danger)] bg-[var(--ov-danger-soft)] text-lg font-semibold text-[var(--ov-danger)]"
        >
          !
        </span>
        <div>
          <h2 className="text-[19px] font-medium leading-snug tracking-[-0.005em] text-ink">
            {t('heading')}
          </h2>
          <p className="mt-1.5 text-sm leading-relaxed text-ink-mid">{t('body')}</p>
        </div>
        <div className="flex flex-wrap items-center justify-center gap-2">
          <Button variant="primary" size="md" onClick={handleRetry}>
            {t('retry')}
          </Button>
          {statusUrl && (
            <Button asChild variant="secondary" size="md">
              <a href={statusUrl}>{t('openStatus')}</a>
            </Button>
          )}
        </div>
        <MonoLabel className="mt-1 normal-case tracking-[0.02em]">
          {t('codeLabel')} <span className="text-ink-mid">{code}</span>
          {now && <span className="text-ink-mid"> · {now}</span>}
        </MonoLabel>
      </div>
    </div>
  );
}
