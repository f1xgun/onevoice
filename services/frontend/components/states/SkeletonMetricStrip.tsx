// components/states/SkeletonMetricStrip.tsx — stat-strip loader.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Skeleton: карточки метрик" (lines 240–254). Used while
// /posts (and similar) compute aggregate counters.

import * as React from 'react';
import { useTranslations } from 'next-intl';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

// Default Tailwind grid is 4-up on md+; for any other count we
// switch to an explicit `repeat(count, …)` track so the strip stays
// rectangular instead of wrapping awkwardly.
const DEFAULT_GRID_TRACK_COUNT = 4;

export interface SkeletonMetricStripProps {
  /** Number of metric cards. Default 4 — matches /posts. */
  count?: number;
  className?: string;
}

export function SkeletonMetricStrip({ count = 4, className }: SkeletonMetricStripProps) {
  const tSkeleton = useTranslations('states.skeleton');
  return (
    <div
      role="status"
      aria-label={tSkeleton('metricStrip')}
      aria-busy="true"
      className={cn('grid grid-cols-2 gap-3 md:grid-cols-4', className)}
      style={{
        gridTemplateColumns:
          count <= DEFAULT_GRID_TRACK_COUNT ? undefined : `repeat(${count}, minmax(0, 1fr))`,
      }}
    >
      {Array.from({ length: count }, (_, i) => (
        <div
          key={i}
          className="flex flex-col gap-3 rounded-md border border-line bg-paper-raised px-5 py-5"
        >
          <Skeleton className="h-[9px] w-[70px]" />
          <Skeleton className="h-7 w-[90px]" />
          <Skeleton className="h-2 w-[120px] opacity-60" />
        </div>
      ))}
    </div>
  );
}
