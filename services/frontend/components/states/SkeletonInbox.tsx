// components/states/SkeletonInbox.tsx — list-of-grey-rows loader.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Skeleton: список инбокса" (lines 217–238). Static (no shimmer)
// per the loading-states header rule. Heights/widths approximate
// the real inbox row: avatar disc + two-line text + right-side
// timestamp.

import * as React from 'react';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

export interface SkeletonInboxProps {
  /** Row count. Default 4 — matches the mock. */
  rows?: number;
  className?: string;
}

export function SkeletonInbox({ rows = 4, className }: SkeletonInboxProps) {
  return (
    <div
      role="status"
      aria-label="Загружаем список"
      aria-live="polite"
      aria-busy="true"
      className={cn('overflow-hidden rounded-lg border border-line bg-paper-raised', className)}
    >
      {Array.from({ length: rows }, (_, i) => (
        <div
          key={i}
          className={cn(
            'grid grid-cols-[24px_1fr_80px] items-center gap-4 px-5 py-4',
            i < rows - 1 && 'border-b border-line-soft'
          )}
        >
          <Skeleton className="h-6 w-6 rounded-full" />
          <div className="flex flex-col gap-2">
            {/* Width pseudo-randomised by index so the rows don't read
                like a perfect rectangle. Static — no animation. */}
            <Skeleton className="h-[11px]" style={{ width: `${30 + ((i * 7) % 25)}%` }} />
            <Skeleton
              className="h-[9px] opacity-60"
              style={{ width: `${55 + ((i * 11) % 30)}%` }}
            />
          </div>
          <Skeleton className="h-[9px] w-[50px]" />
        </div>
      ))}
    </div>
  );
}
