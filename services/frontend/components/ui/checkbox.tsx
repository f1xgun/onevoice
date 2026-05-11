'use client';

import * as React from 'react';
import * as CheckboxPrimitive from '@radix-ui/react-checkbox';
import { Check, Minus } from 'lucide-react';

import { cn } from '@/lib/utils';

// Phase 5 / Plan 05-05 — primitive extended to render BOTH a Check glyph and
// a Minus glyph inside the Indicator. Tailwind's `group/cb` + the arbitrary
// `group-data-[state=*]/cb:*` variant flip visibility based on Radix's
// `data-state` attribute (Tailwind v3.3+ supports the named-group syntax;
// we pin v3.4.1 in package.json).
//
//   data-state="checked"        → Check visible, Minus hidden
//   data-state="indeterminate"  → Check hidden,  Minus visible
//   data-state="unchecked"      → Indicator itself is hidden by Radix
//
// Existing call sites (Phase 4 invitation forms, members tab) keep working —
// the props API is unchanged; we only added one Tailwind class and a second
// glyph SVG inside the Indicator. See RESEARCH §"Radix Checkbox `indeterminate`
// wiring" for the syntax rationale and the imperative-MutationObserver
// fallback (not needed since v3.4.1 is pinned).
const Checkbox = React.forwardRef<
  React.ElementRef<typeof CheckboxPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root>
>(({ className, ...props }, ref) => (
  <CheckboxPrimitive.Root
    ref={ref}
    className={cn(
      // Linen motion + focus: 120ms ease-out, focus-visible ochre ring (2px + 2px offset).
      // `group/cb` names the root so child SVGs can read its data-state.
      // The `data-[state=indeterminate]:*` pair mirrors the checked-state
      // visual so a partially-selected group looks the same intensity as a
      // fully-selected one (UI-SPEC §S-3).
      'group/cb duration-[120ms] peer grid h-4 w-4 shrink-0 place-content-center rounded-sm border border-primary shadow ring-offset-background transition-colors ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-primary data-[state=indeterminate]:bg-primary data-[state=checked]:text-primary-foreground data-[state=indeterminate]:text-primary-foreground',
      className
    )}
    {...props}
  >
    <CheckboxPrimitive.Indicator className={cn('grid place-content-center text-current')}>
      <Check className="h-4 w-4 group-data-[state=indeterminate]/cb:hidden" />
      <Minus className="hidden h-4 w-4 group-data-[state=indeterminate]/cb:block" />
    </CheckboxPrimitive.Indicator>
  </CheckboxPrimitive.Root>
));
Checkbox.displayName = CheckboxPrimitive.Root.displayName;

export { Checkbox };
