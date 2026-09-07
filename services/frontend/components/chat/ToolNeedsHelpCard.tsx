'use client';

import { CircleHelp } from 'lucide-react';
import * as React from 'react';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';
import { ActionButton as Button } from '@/components/design-system/ActionButton';

export interface ToolNeedsHelpCardProps {
  /** Mono tool name — e.g. `review.draft_reply`. */
  toolName: string;
  /**
   * Plain-Russian explanation of WHY the agent stopped. Brand voice:
   * what happened, why it's not scary, what's needed.
   */
  message: React.ReactNode;
  /** Primary action handler — opens the help context. */
  onHelp: () => void;
  /** Secondary action — usually "Дать контекст" or similar. */
  onProvideContext?: () => void;
  /** Override the default "Помочь" / "Дать контекст" copy. */
  helpLabel?: string;
  contextLabel?: string;
  className?: string;
}

export function ToolNeedsHelpCard({
  toolName,
  message,
  onHelp,
  onProvideContext,
  helpLabel,
  contextLabel,
  className,
}: ToolNeedsHelpCardProps) {
  const tHelp = useTranslations('chat.toolNeedsHelp');
  const resolvedHelpLabel = helpLabel ?? tHelp('helpAction');
  const resolvedContextLabel = contextLabel ?? tHelp('contextAction');
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        'rounded-md border border-line bg-paper-raised p-4 text-sm text-ink shadow-ov-1',
        className
      )}
    >
      <div className="mb-2 flex flex-wrap items-center gap-2.5">
        <CircleHelp aria-hidden className="h-5 w-5 shrink-0 text-warning" />
        <span className="min-w-0 break-all font-mono text-technical text-ink">{toolName}</span>
        <span
          className={cn(
            'ml-auto inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium',
            'bg-warning-soft text-[var(--ov-warning-ink)]'
          )}
        >
          <span
            aria-hidden="true"
            className="h-[6px] w-[6px] rounded-full bg-[var(--ov-warning)]"
          />
          {tHelp('badge')}
        </span>
      </div>
      <p className="leading-relaxed text-ink">{message}</p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant="primary" size="sm" onClick={onHelp}>
          {resolvedHelpLabel}
        </Button>
        {onProvideContext && (
          <Button variant="secondary" size="sm" onClick={onProvideContext}>
            {resolvedContextLabel}
          </Button>
        )}
      </div>
    </div>
  );
}
