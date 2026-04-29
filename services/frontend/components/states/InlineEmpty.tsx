// components/states/InlineEmpty.tsx — small empty state for sub-cards.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Inline пустота" row (lines 158–171). Renders inside an existing
// section header card — the section header is the caller's
// responsibility; this component is just the centred quiet copy.
//
// Two shapes:
//   1. `<InlineEmpty>Пока ничего не запланировано.</InlineEmpty>` — pure
//      paper-soft inline placeholder.
//   2. With `wrapped` — adds the paper-raised shell with a header slot
//      so callers can drop a single self-contained block ("title +
//      action + empty body").

import * as React from 'react';
import { cn } from '@/lib/utils';

export interface InlineEmptyProps {
  children: React.ReactNode;
  className?: string;
}

export function InlineEmpty({ children, className }: InlineEmptyProps) {
  return (
    <div
      className={cn(
        'px-5 py-10 text-center text-sm text-ink-soft',
        className
      )}
    >
      {children}
    </div>
  );
}

export interface InlineEmptySectionProps {
  /** Title shown in the small header strip at the top of the card. */
  title: React.ReactNode;
  /** Optional right-aligned action (button etc.). */
  action?: React.ReactNode;
  /** Empty body copy — single short sentence. */
  children: React.ReactNode;
  className?: string;
}

/**
 * Wrapped variant — paper-raised card with a header strip and the
 * inline empty inside. Use this when the page doesn't already supply
 * the section header (e.g. project sub-trees, detail-page inner cards).
 */
export function InlineEmptySection({
  title,
  action,
  children,
  className,
}: InlineEmptySectionProps) {
  return (
    <div
      className={cn(
        'overflow-hidden rounded-lg border border-line bg-paper-raised',
        className
      )}
    >
      <div className="flex items-center justify-between border-b border-line-soft px-5 py-3.5">
        <span className="text-[15px] font-semibold text-ink">{title}</span>
        {action}
      </div>
      <InlineEmpty>{children}</InlineEmpty>
    </div>
  );
}
