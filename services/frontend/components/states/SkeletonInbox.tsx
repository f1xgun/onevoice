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

// Pseudo-randomised widths (% of available row space) for the two text
// lines. Title gets a base of 30 % with up to +24 % jitter; subtitle gets
// a base of 55 % with up to +29 % jitter. The jitter is computed by
// (i * primeStep) % jitterRange so the rows feel uneven without using
// Math.random (no SSR mismatch). Steps are coprime with the modulus to
// spread the values evenly across rows.
const TITLE_BASE_PCT = 30;
const TITLE_JITTER_STEP = 7;
const TITLE_JITTER_RANGE_PCT = 25;
const SUBTITLE_BASE_PCT = 55;
const SUBTITLE_JITTER_STEP = 11;
const SUBTITLE_JITTER_RANGE_PCT = 30;

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
            <Skeleton
              className="h-[11px]"
              style={{
                width: `${TITLE_BASE_PCT + ((i * TITLE_JITTER_STEP) % TITLE_JITTER_RANGE_PCT)}%`,
              }}
            />
            <Skeleton
              className="h-[9px] opacity-60"
              style={{
                width: `${SUBTITLE_BASE_PCT + ((i * SUBTITLE_JITTER_STEP) % SUBTITLE_JITTER_RANGE_PCT)}%`,
              }}
            />
          </div>
          <Skeleton className="h-[9px] w-[50px]" />
        </div>
      ))}
    </div>
  );
}
