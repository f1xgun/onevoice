// components/states/SkeletonChannels.tsx — /integrations grid loader.
//
// No literal mock — extrapolated from mock-integrations-v2.jsx
// PlatformCard layout. Static paper-sunken blocks shaped like the
// real card: square mark + title + description + CTA strip.

import * as React from 'react';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

export interface SkeletonChannelsProps {
  /** Number of platform cards. Default 3 (Telegram + VK + Yandex). */
  count?: number;
  className?: string;
}

export function SkeletonChannels({ count = 3, className }: SkeletonChannelsProps) {
  return (
    <div
      role="status"
      aria-label="Загружаем подключённые каналы"
      aria-busy="true"
      className={cn('grid grid-cols-1 gap-4 md:grid-cols-2', className)}
    >
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="overflow-hidden rounded-lg border border-line bg-paper-raised">
          <div className="flex items-start gap-4 px-5 py-5">
            <Skeleton className="h-10 w-10 rounded-md" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-[14px] w-[120px]" />
              <Skeleton className="h-[11px] w-[200px] opacity-70" />
            </div>
          </div>
          <div className="px-5 pb-5">
            <Skeleton className="h-8 w-[120px] rounded-md" />
          </div>
        </div>
      ))}
    </div>
  );
}
