// components/ui/skeleton.tsx — OneVoice (Linen) override
//
// Linen mandates static skeletons: structure-shaped paper-sunken blocks,
// no shimmer, no spinners. Per design_handoff_onevoice 2/mocks/mock-states.jsx
// loading section: "Никакого shimmer и spinner-овражей. Скелетоны статичны
// и используют line-цвет; индикатор прогресса — медленный, ненавязчивый."
//
// We keep the export name `Skeleton` so existing call-sites continue to
// work; only the visual changes (no animate-pulse, paper-sunken fill).

import * as React from 'react';
import { cn } from '@/lib/utils';

function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      data-state="static"
      aria-hidden="true"
      className={cn('rounded-md bg-paper-sunken', className)}
      {...props}
    />
  );
}

export { Skeleton };
