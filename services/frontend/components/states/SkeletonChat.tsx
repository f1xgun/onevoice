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
              width: `${100 - i * 18}%`,
              opacity: Math.max(0.3, 0.7 - i * 0.1),
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
      className={cn(
        'flex flex-col gap-3.5 rounded-lg bg-paper-well p-6',
        className
      )}
    >
      <Bubble side="left" width="60%" lines={2} />
      <Bubble side="left" width="42%" lines={1} />
      <Bubble side="right" width="55%" lines={2} />
      <Bubble side="left" width="68%" lines={3} />
    </div>
  );
}
