// components/states/NoConnection.tsx — OneVoice (Linen) offline state
//
// Full-screen "не получается дотянуться" frame. Mirrors the
// "Полноэкранная: нет связи" panel from
// design_handoff_onevoice 2/mocks/mock-states.jsx (ErrorStatesPage):
// calm paper background, big graphite headline, one-line sub, link to
// the status page placeholder, and a mono error code at the bottom.
//
// Render this when the API/orchestrator is provably unreachable — not
// for transient 5xx that one retry would fix. The default is to call
// `window.location.reload()` on retry; pass `onRetry` to override.
//
// TODO(api): there is no orchestrator-level "is the platform alive"
// probe today. When one lands, hook this component up to it via a
// thin client-side hook (e.g. useOnlineStatus) and render at the layout
// level so every authenticated route benefits. Until then, this lives
// as a manual fallback that pages can mount in their `error.tsx`
// boundary or in places where a network failure is the expected outcome.

'use client';

import * as React from 'react';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';
import { cn } from '@/lib/utils';

export interface NoConnectionProps {
  /** Override the default "reload the page" behavior. */
  onRetry?: () => void;
  /** Where the "Открыть статус" button points. Defaults to `/status`. */
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

function formatTime(d: Date) {
  return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });
}

export function NoConnection({
  onRetry,
  statusUrl = '/status',
  code = 'NET_UNREACHABLE',
  timestamp,
  fullscreen,
  className,
}: NoConnectionProps) {
  const handleRetry = React.useCallback(() => {
    if (onRetry) {
      onRetry();
      return;
    }
    if (typeof window !== 'undefined') {
      window.location.reload();
    }
  }, [onRetry]);

  // useState lazy-init so the timestamp is computed once on mount and
  // doesn't change between SSR and client hydration. Server renders an
  // empty string; the effect below fills it in.
  const [now, setNow] = React.useState<string>(timestamp ?? '');
  React.useEffect(() => {
    if (!timestamp) setNow(formatTime(new Date()));
  }, [timestamp]);

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
            Не получается дотянуться до OneVoice
          </h2>
          <p className="mt-1.5 text-sm leading-relaxed text-ink-mid">
            Похоже, проблема на нашей стороне. Мы уже знаем — статус-страница обновляется в реальном
            времени.
          </p>
        </div>
        <div className="flex flex-wrap items-center justify-center gap-2">
          <Button variant="primary" size="md" onClick={handleRetry}>
            Попробовать снова
          </Button>
          <Button asChild variant="secondary" size="md">
            <a href={statusUrl}>Открыть статус</a>
          </Button>
        </div>
        <MonoLabel className="mt-1 normal-case tracking-[0.02em]">
          код: <span className="text-ink-mid">{code}</span>
          {now && <span className="text-ink-mid"> · {now}</span>}
        </MonoLabel>
      </div>
    </div>
  );
}
