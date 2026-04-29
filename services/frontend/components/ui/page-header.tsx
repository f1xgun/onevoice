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
        // Stacks vertically on phones so a long title + actions don't share
        // one cramped row. Padding shrinks to 16 px on mobile.
        'flex flex-col items-stretch gap-3 px-4 pt-6 pb-5 sm:flex-row sm:items-end sm:justify-between sm:gap-6 sm:px-12 sm:pt-10 sm:pb-6',
        className
      )}
    >
      <div className="min-w-0">
        <h1 className="text-[24px] font-medium leading-tight tracking-[-0.015em] text-ink sm:text-[28px]">
          {title}
        </h1>
        {sub && (
          <p className="mt-1 max-w-[640px] text-sm leading-relaxed text-ink-mid">
            {sub}
          </p>
        )}
      </div>
      {actions && (
        <div className="flex flex-wrap items-center gap-2 sm:shrink-0">{actions}</div>
      )}
    </header>
  );
}
