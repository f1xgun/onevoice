// components/states/SkeletonChat.tsx — AI conversation loader.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Skeleton: разговор с AI" (lines 256–263). Used in /chat/[id]
// before message hydration completes — replaces the previous
// indeterminate spinner.

import * as React from 'react';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

interface BubbleProps {
  side: 'left' | 'right';
  width: string;
  lines?: number;
}

// Per-line bubble geometry — paper-sunken proportions per the Linen
// loading rule. Each successive line in a bubble is narrower
// (BUBBLE_LINE_WIDTH_STEP_PCT %) and lighter (BUBBLE_LINE_OPACITY_STEP)
// than the previous one, with a floor at BUBBLE_LINE_OPACITY_MIN so the
// trailing line never disappears entirely.
const BUBBLE_LINE_WIDTH_STEP_PCT = 18;
const BUBBLE_LINE_OPACITY_BASE = 0.7;
const BUBBLE_LINE_OPACITY_STEP = 0.1;
const BUBBLE_LINE_OPACITY_MIN = 0.3;
const FULL_WIDTH_PCT = 100;

function Bubble({ side, width, lines = 2 }: BubbleProps) {
  return (
    <div className={cn('flex', side === 'right' ? 'justify-end' : 'justify-start')}>
      <div
        className={cn(
          'flex flex-col gap-1.5 rounded-md border border-line-soft p-3',
          side === 'right' ? 'bg-paper-sunken' : 'bg-paper-raised'
        )}
        style={{ width }}
      >
        {Array.from({ length: lines }, (_, i) => (
          <Skeleton
            key={i}
            className="h-[9px]"
            style={{
              width: `${FULL_WIDTH_PCT - i * BUBBLE_LINE_WIDTH_STEP_PCT}%`,
              opacity: Math.max(
                BUBBLE_LINE_OPACITY_MIN,
                BUBBLE_LINE_OPACITY_BASE - i * BUBBLE_LINE_OPACITY_STEP
              ),
            }}
          />
        ))}
      </div>
    </div>
  );
}

export interface SkeletonChatProps {
  className?: string;
}

export function SkeletonChat({ className }: SkeletonChatProps) {
  return (
    <div
      role="status"
      aria-label="Загружаем диалог"
      aria-busy="true"
      className={cn('flex flex-col gap-3.5 rounded-lg bg-paper-well p-6', className)}
    >
      <Bubble side="left" width="60%" lines={2} />
      <Bubble side="left" width="42%" lines={1} />
      <Bubble side="right" width="55%" lines={2} />
      <Bubble side="left" width="68%" lines={3} />
    </div>
  );
}
