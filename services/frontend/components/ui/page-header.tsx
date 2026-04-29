// components/ui/page-header.tsx — OneVoice (Linen) primitive
// Header used at the top of every dashboard page. Title + optional sub
// + right-aligned actions slot. Locked spacing/sizing per
// design_handoff_onevoice 2/mocks/mock-shell.jsx PageHeader (lines 164–177).

import * as React from 'react';
import { cn } from '@/lib/utils';

export interface PageHeaderProps {
  title: React.ReactNode;
  /** Optional supporting line below the title — ink-mid, max ~640px wide. */
  sub?: React.ReactNode;
  /** Right-aligned slot for buttons / filters / page actions. */
  actions?: React.ReactNode;
  className?: string;
}

export function PageHeader({ title, sub, actions, className }: PageHeaderProps) {
  return (
    <header
      className={cn(
        'flex items-end justify-between gap-6 px-12 pt-10 pb-6',
        className
      )}
    >
      <div className="min-w-0">
        <h1 className="text-[28px] font-medium leading-tight tracking-[-0.015em] text-ink">
          {title}
        </h1>
        {sub && (
          <p className="mt-1 max-w-[640px] text-sm leading-relaxed text-ink-mid">
            {sub}
          </p>
        )}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </header>
  );
}
