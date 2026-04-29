// components/states/InlineSyncPill.tsx — small non-blocking sync indicator.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Inline-индикатор" (lines 312–317). Sits near platform dots /
// integration cards / sidebar headers when something is happening
// in the background.
//
// Per the loading-states header rule: no shimmer. The dot is solid
// info-tone. Callers can pass a tone for warning/danger sync states.

import * as React from 'react';
import { cn } from '@/lib/utils';

type Tone = 'info' | 'success' | 'warning' | 'danger' | 'neutral';

const dotByTone: Record<Tone, string> = {
  info: 'bg-info',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
  neutral: 'bg-ink-faint',
};

export interface InlineSyncPillProps {
  /** Caption text — e.g. "Синхронизируем VK · 14 из 38". */
  children: React.ReactNode;
  tone?: Tone;
  className?: string;
}

export function InlineSyncPill({ children, tone = 'info', className }: InlineSyncPillProps) {
  return (
    <span
      role="status"
      aria-live="polite"
      className={cn(
        'inline-flex w-fit items-center gap-2.5 rounded-full bg-paper-sunken px-3.5 py-1.5 text-[13px] text-ink-mid',
        className
      )}
    >
      <span aria-hidden className={cn('size-2 shrink-0 rounded-full', dotByTone[tone])} />
      {children}
    </span>
  );
}
