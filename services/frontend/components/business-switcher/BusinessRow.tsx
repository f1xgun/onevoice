'use client';

import { forwardRef } from 'react';
import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { BusinessSummary } from '@/lib/hooks/useBusinessList';
import { RolePill } from './RolePill';

export interface BusinessRowProps {
  business: BusinessSummary;
  isActive: boolean;
  onSelect: (b: BusinessSummary) => void;
}

/**
 * Per-business row inside the BusinessSwitcher Popover.
 *
 * Exposes `role="menuitemradio"` with `aria-checked` so screen readers
 * announce the row as a selectable option in a radio group. The 2px ochre
 * absolute bar on the active state mirrors NavRail's nav-link indicator
 * (`components/sidebar/NavRail.tsx` lines 120–125).
 */
export const BusinessRow = forwardRef<HTMLButtonElement, BusinessRowProps>(function BusinessRow(
  { business, isActive, onSelect },
  ref
) {
  const initials = business.name.slice(0, 2).toUpperCase();
  return (
    <button
      ref={ref}
      type="button"
      role="menuitemradio"
      aria-checked={isActive}
      data-roving-item
      onClick={() => onSelect(business)}
      className={cn(
        'relative flex w-full items-center gap-3 rounded-md px-3 py-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
        isActive ? 'text-ink' : 'text-ink-mid hover:bg-paper-sunken hover:text-ink'
      )}
    >
      {isActive && (
        <span aria-hidden className="absolute -left-1 bottom-2 top-2 w-0.5 rounded-r bg-ochre" />
      )}
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-paper-sunken text-xs font-medium tracking-tight text-ink">
        {initials}
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm text-ink">{business.name}</span>
        <RolePill roleName={business.role.name} />
      </span>
      {isActive && <Check size={16} className="shrink-0 text-ochre" aria-hidden />}
    </button>
  );
});
