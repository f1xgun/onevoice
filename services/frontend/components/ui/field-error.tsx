// components/ui/field-error.tsx — OneVoice (Linen) field-error primitive
//
// Inline error message for form fields. Mirrors the inline pattern from
// design_handoff_onevoice 2/mocks/mock-states.jsx (ErrorStatesPage,
// "Inline-ошибка поля"): a small ochre/danger dot followed by a calm,
// 13 px explanation in plain Russian. Use under any input that needs an
// inline validation message — react-hook-form, controlled inputs, etc.
//
// Renders nothing when no children are provided so it is safe to mount
// unconditionally next to a field.

import * as React from 'react';
import { cn } from '@/lib/utils';

export interface FieldErrorProps extends React.HTMLAttributes<HTMLParagraphElement> {
  /**
   * Hide the leading dot. Defaults to `false` (dot is shown). The dot
   * keeps the inline error visually distinct from helper-text below
   * inputs without resorting to bold red.
   */
  hideDot?: boolean;
}

export function FieldError({ children, className, hideDot, ...props }: FieldErrorProps) {
  if (!children) return null;
  return (
    <p
      className={cn(
        'flex items-center gap-1.5 text-[13px] leading-snug text-[var(--ov-danger)]',
        className
      )}
      role="alert"
      {...props}
    >
      {!hideDot && (
        <span
          aria-hidden="true"
          className="inline-block h-[6px] w-[6px] shrink-0 rounded-full bg-[var(--ov-danger)]"
        />
      )}
      <span className="min-w-0">{children}</span>
    </p>
  );
}
