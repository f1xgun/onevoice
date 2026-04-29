// components/states/EmptyFrame.tsx — Linen empty-state shell
//
// Shared layout primitive for the screen-level empty states defined in
// design_handoff_onevoice 2/mocks/mock-states.jsx EmptyFrame (lines 176–186).
// One sentence + one action max — copy lives in the caller.
//
// Variants:
//   - default: dashed paper-raised card, generous padding
//   - compact: same, smaller padding (used by inline filter empties)

import * as React from 'react';
import { cn } from '@/lib/utils';

export interface EmptyFrameProps {
  /** Heading — short, factual. Brand voice: no celebration, no emoji. */
  title: React.ReactNode;
  /** Supporting line — at most one sentence. Optional. */
  body?: React.ReactNode;
  /** Single CTA / action slot. Empty states get one action max. */
  action?: React.ReactNode;
  /** Mark variant — solid (default), dashed (first-run), or none. */
  mark?: 'solid' | 'dashed' | 'none';
  /** Pixel size of the round mark. Default 56 — compact uses 36. */
  markSize?: number;
  /** Smaller padding for filter-level empties inside a page. */
  compact?: boolean;
  className?: string;
}

export function EmptyFrame({
  title,
  body,
  action,
  mark = 'solid',
  markSize,
  compact = false,
  className,
}: EmptyFrameProps) {
  const size = markSize ?? (compact ? 36 : 56);
  return (
    <div
      className={cn(
        'flex flex-col items-center gap-4 rounded-lg border border-dashed border-line bg-paper-raised text-center',
        compact ? 'px-7 py-8' : 'px-8 py-14',
        className
      )}
    >
      {mark !== 'none' && (
        <span
          aria-hidden
          className={cn(
            'rounded-full bg-paper-sunken',
            mark === 'dashed'
              ? 'border-[1.5px] border-dashed border-line'
              : 'border border-line'
          )}
          style={{ width: size, height: size }}
        />
      )}
      <div className="max-w-[380px]">
        <h3 className="text-[19px] font-medium leading-snug tracking-[-0.005em] text-ink">
          {title}
        </h3>
        {body && (
          <p className="mt-1.5 text-sm leading-relaxed text-ink-soft">{body}</p>
        )}
      </div>
      {action && <div className="flex flex-wrap items-center justify-center gap-2">{action}</div>}
    </div>
  );
}
