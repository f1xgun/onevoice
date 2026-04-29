// components/ui/badge.tsx — OneVoice (Linen) override
// Adds a "dot" variant that puts a colored disc before the label.
//
// Backwards-compat: legacy callers passing the shadcn `variant` prop
// (default | secondary | destructive | outline) continue to work —
// the prop is mapped to the new `tone` model.

import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';

const badgeVariants = cva(
  'inline-flex items-center gap-1.5 rounded-full px-2.5 h-[22px] text-[11px] font-medium tracking-[0.01em] transition-colors',
  {
    variants: {
      tone: {
        neutral: 'bg-[var(--ov-paper-sunken)] text-[var(--ov-ink-mid)]',
        accent: 'bg-[var(--ov-accent-soft)]  text-[var(--ov-accent-ink)]',
        success: 'bg-[var(--ov-success-soft)] text-[var(--ov-success)]',
        warning: 'bg-[var(--ov-warning-soft)] text-[var(--ov-warning-ink)]',
        danger: 'bg-[var(--ov-danger-soft)]  text-[var(--ov-danger)]',
        info: 'bg-[var(--ov-info-soft)]    text-[var(--ov-info)]',
      },
    },
    defaultVariants: { tone: 'neutral' },
  }
);

type Tone = NonNullable<VariantProps<typeof badgeVariants>['tone']>;

const dotColor: Record<Tone, string> = {
  neutral: 'bg-[var(--ov-ink-soft)]',
  accent: 'bg-[var(--ov-accent)]',
  success: 'bg-[var(--ov-success)]',
  warning: 'bg-[var(--ov-warning)]',
  danger: 'bg-[var(--ov-danger)]',
  info: 'bg-[var(--ov-info)]',
};

// Legacy variant → new tone. `outline` collapses to neutral; the visual
// difference (border) is dropped in Linen — neutral already reads as a
// quiet pill on the warm paper background.
const legacyVariantToTone: Record<string, Tone> = {
  default: 'accent',
  secondary: 'neutral',
  destructive: 'danger',
  outline: 'neutral',
};

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  tone?: Tone;
  /** @deprecated Use `tone` instead. Mapped automatically: default→accent, secondary→neutral, destructive→danger, outline→neutral. */
  variant?: 'default' | 'secondary' | 'destructive' | 'outline';
  dot?: boolean;
}

function Badge({ className, tone, variant, dot, children, ...props }: BadgeProps) {
  const resolvedTone: Tone =
    tone ?? (variant ? legacyVariantToTone[variant] : undefined) ?? 'neutral';
  return (
    <span className={cn(badgeVariants({ tone: resolvedTone }), className)} {...props}>
      {dot && <span className={cn('size-1.5 rounded-full', dotColor[resolvedTone])} aria-hidden />}
      {children}
    </span>
  );
}

export { Badge, badgeVariants };
