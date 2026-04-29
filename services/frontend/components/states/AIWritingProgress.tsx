// components/states/AIWritingProgress.tsx — "OneVoice пишет черновик…".
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Прогресс: AI пишет ответ" (lines 265–278). Used inside tool-call /
// draft surfaces while the assistant streams a long-running answer.
//
// Per the loading-states header rule: progress is "медленный, ненавязчивый" —
// no shimmer, no animated dots; just a static dot, calm copy, optional
// determinate bar and a mono ETA caption.

import * as React from 'react';
import { MonoLabel } from '@/components/ui/mono-label';
import { cn } from '@/lib/utils';

export interface AIWritingProgressProps {
  /** Headline copy. Default per mock. */
  label?: React.ReactNode;
  /** Optional ETA caption shown at the right edge — e.g. "~ 4 сек". */
  eta?: React.ReactNode;
  /** 0..1 — when set, renders the thin progress rail under the row. */
  progress?: number;
  className?: string;
}

export function AIWritingProgress({
  label = 'OneVoice пишет черновик…',
  eta,
  progress,
  className,
}: AIWritingProgressProps) {
  const pct =
    typeof progress === 'number'
      ? Math.max(0, Math.min(1, progress)) * 100
      : null;

  return (
    <div
      role="status"
      aria-live="polite"
      aria-busy="true"
      className={cn('flex max-w-[480px] flex-col gap-2', className)}
    >
      <div className="flex items-center gap-3 rounded-md border border-line bg-paper-raised px-4 py-3.5">
        <span aria-hidden className="size-2 shrink-0 rounded-full bg-ochre" />
        <span className="text-sm text-ink">{label}</span>
        {eta && (
          <span className="ml-auto">
            <MonoLabel>{eta}</MonoLabel>
          </span>
        )}
      </div>
      {pct != null && (
        <div className="h-0.5 w-full overflow-hidden rounded-full bg-paper-sunken">
          <div
            className="h-full bg-ochre transition-[width] duration-300"
            style={{ width: `${pct}%` }}
          />
        </div>
      )}
    </div>
  );
}
