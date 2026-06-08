// components/ui/form-error.tsx — OneVoice (Linen) form-level error banner.
//
// Whole-form error surface for auth / API failures (invalid credentials,
// email already taken, server-side validation). Distinct from FieldError,
// which annotates a single field: this renders a calm danger-bordered block
// inside the form so the message stays readable in place instead of a
// transient toast that scrolls past or sits off-screen.
//
// Renders nothing when no children are provided so it is safe to mount
// unconditionally above a submit button.

import * as React from 'react';
import { cn } from '@/lib/utils';

export function FormError({ children, className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  if (!children) return null;
  return (
    <div
      role="alert"
      className={cn(
        'flex items-start gap-2 rounded-md border border-[var(--ov-danger)] bg-paper-sunken px-3.5 py-2.5 text-sm leading-snug text-ink',
        className
      )}
      {...props}
    >
      <span
        aria-hidden="true"
        className="mt-1 inline-block h-[6px] w-[6px] shrink-0 rounded-full bg-[var(--ov-danger)]"
      />
      <span className="min-w-0">{children}</span>
    </div>
  );
}
