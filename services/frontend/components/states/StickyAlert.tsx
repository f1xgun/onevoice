// components/states/StickyAlert.tsx — OneVoice (Linen) sticky banner
//
// Mirrors the "Баннер: канал отвалился" frame from
// design_handoff_onevoice 2/mocks/mock-states.jsx (ErrorStatesPage):
// warning-toned banner that sticks to the top of the route's main
// scroll container until the underlying issue is resolved or the user
// dismisses it. NOT a viewport-pinned banner — it's `sticky top-0`
// inside the page content so the NavRail keeps its own corner.
//
// Use sparingly — one banner at a time. If multiple integrations are
// broken, surface a single banner that links to /integrations rather
// than stacking three banners on top of every page.

'use client';

import * as React from 'react';
import { X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';

export type StickyAlertTone = 'warning' | 'danger' | 'info';

export interface StickyAlertProps {
  /** Single-line headline. Plain Russian, no exclamation. */
  title: React.ReactNode;
  /** Optional one-sentence explanation. */
  description?: React.ReactNode;
  /** Primary action (e.g. "Переподключить"). Optional. */
  action?: {
    label: string;
    onClick: () => void;
  };
  /**
   * If provided, renders a small `×` button on the right that calls
   * this. Banners that need the user to act (auth lost) usually skip
   * this; banners that are purely informational include it.
   */
  onDismiss?: () => void;
  tone?: StickyAlertTone;
  className?: string;
}

const toneClasses: Record<
  StickyAlertTone,
  { bg: string; border: string; text: string; dot: string }
> = {
  warning: {
    bg: 'bg-warning-soft',
    border: 'border-[oklch(0.85_0.10_75)]',
    text: 'text-[var(--ov-warning-ink)]',
    dot: 'bg-[var(--ov-warning)]',
  },
  danger: {
    bg: 'bg-[var(--ov-danger-soft)]',
    border: 'border-[oklch(0.85_0.08_25)]',
    text: 'text-[var(--ov-danger)]',
    dot: 'bg-[var(--ov-danger)]',
  },
  info: {
    bg: 'bg-info-soft',
    border: 'border-[oklch(0.85_0.05_230)]',
    text: 'text-[var(--ov-ink)]',
    dot: 'bg-[var(--ov-info)]',
  },
};

export function StickyAlert({
  title,
  description,
  action,
  onDismiss,
  tone = 'warning',
  className,
}: StickyAlertProps) {
  const t = toneClasses[tone];
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        // Sticky inside the route content so the NavRail keeps its own
        // corner. z-10 keeps the banner above plain page content but
        // below sheets/modals (z-50 in shadcn primitives).
        'sticky top-0 z-10',
        'flex items-center gap-4 border-b px-6 py-3',
        t.bg,
        t.border,
        t.text,
        className
      )}
    >
      <span
        aria-hidden="true"
        className={cn('h-2 w-2 shrink-0 rounded-full', t.dot)}
      />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium leading-snug">{title}</div>
        {description && (
          <div className="mt-0.5 text-[13px] leading-snug opacity-85">
            {description}
          </div>
        )}
      </div>
      {action && (
        <Button
          variant="primary"
          size="sm"
          onClick={action.onClick}
          className="shrink-0"
        >
          {action.label}
        </Button>
      )}
      {onDismiss && (
        <button
          type="button"
          aria-label="Скрыть уведомление"
          onClick={onDismiss}
          className={cn(
            'shrink-0 rounded-md p-1 transition-colors',
            'hover:bg-[oklch(0.30_0.02_60_/_0.06)]',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1'
          )}
        >
          <X className="h-4 w-4" />
        </button>
      )}
    </div>
  );
}
