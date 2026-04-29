// components/chat/ToolNeedsHelpCard.tsx — OneVoice (Linen) "needs help"
//
// Mirrors the "AI отказался от задачи" frame from
// design_handoff_onevoice 2/mocks/mock-states.jsx (ErrorStatesPage):
// rendered inside a chat thread when an agent task can't complete on
// its own — usually because the model isn't sure how to answer and
// would rather defer to the operator than guess.
//
// Public contract is intentionally minimal — this is a render-only
// card. The wiring (when to show it, what "Помочь" actually does) is
// out of scope for the design pass; call-sites pass an `onHelp`
// callback and a plain-Russian explanation, the card paints them.

'use client';

import * as React from 'react';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';

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
  helpLabel = 'Помочь',
  contextLabel = 'Дать контекст',
  className,
}: ToolNeedsHelpCardProps) {
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        'rounded-md border border-line bg-paper-raised p-4 text-sm text-ink shadow-ov-1',
        className
      )}
    >
      <div className="mb-2 flex items-center gap-2.5">
        <span
          aria-hidden="true"
          className="inline-block h-[22px] w-[22px] shrink-0 rounded-md border border-line-soft bg-paper-sunken"
        />
        <span className="truncate font-mono text-[13px] font-medium text-ink">{toolName}</span>
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
          нужна помощь
        </span>
      </div>
      <p className="leading-relaxed text-ink">{message}</p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button variant="primary" size="sm" onClick={onHelp}>
          {helpLabel}
        </Button>
        {onProvideContext && (
          <Button variant="secondary" size="sm" onClick={onProvideContext}>
            {contextLabel}
          </Button>
        )}
      </div>
    </div>
  );
}
