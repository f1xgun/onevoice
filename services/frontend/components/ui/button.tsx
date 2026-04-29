// components/ui/button.tsx — OneVoice (Linen) override
// Drop-in replacement for the shadcn default. Adds an "accent" variant
// (terracotta) for moments of true emphasis. Default "primary" is graphite.
//
// Backwards-compat: legacy callers using variant="default" or
// variant="destructive" continue to work — they alias to primary/danger.

import * as React from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';

const buttonVariants = cva(
  [
    'inline-flex items-center justify-center gap-2 whitespace-nowrap',
    'rounded-md font-medium tracking-[-0.005em]',
    'ring-offset-background transition-[background,border-color,color] duration-150',
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2',
    'disabled:pointer-events-none disabled:opacity-50',
    "[&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  ].join(' '),
  {
    variants: {
      variant: {
        // Graphite — the default action. Calm, confident.
        primary:
          'bg-[var(--ov-ink)] text-[var(--ov-paper)] border border-[var(--ov-ink)] hover:bg-[oklch(0.28_0.012_60)]',
        // Terracotta — reserved for moments of emphasis (Connect, Publish)
        accent:
          'bg-[var(--ov-accent)] text-[oklch(0.99_0_0)] border border-[var(--ov-accent-deep)] hover:bg-[var(--ov-accent-deep)]',
        secondary:
          'bg-[var(--ov-paper-raised)] text-[var(--ov-ink)] border border-[var(--ov-line)] hover:bg-[var(--ov-paper-sunken)]',
        ghost:
          'bg-transparent text-[var(--ov-ink-mid)] hover:bg-[var(--ov-paper-sunken)] hover:text-[var(--ov-ink)]',
        outline:
          'bg-transparent text-[var(--ov-ink)] border border-[var(--ov-line)] hover:bg-[var(--ov-paper-sunken)]',
        danger:
          'bg-[var(--ov-paper-raised)] text-[var(--ov-danger)] border border-[var(--ov-line)] hover:bg-[var(--ov-danger-soft)]',
        link:
          'bg-transparent text-[var(--ov-accent)] underline-offset-4 hover:underline px-0',
        // ─── Backwards-compat aliases (deprecated) ──────────────────
        // `default` → `primary`. `destructive` → `danger`.
        // Per-page rebuilds in Phase 4 will migrate call-sites.
        default:
          'bg-[var(--ov-ink)] text-[var(--ov-paper)] border border-[var(--ov-ink)] hover:bg-[oklch(0.28_0.012_60)]',
        destructive:
          'bg-[var(--ov-paper-raised)] text-[var(--ov-danger)] border border-[var(--ov-line)] hover:bg-[var(--ov-danger-soft)]',
      },
      size: {
        sm: 'h-8 px-3 text-[13px]',
        md: 'h-[38px] px-4 text-sm',
        lg: 'h-[46px] px-5 text-[15px]',
        icon: 'h-[38px] w-[38px]',
        // Backwards-compat: legacy `size="default"` (none in tree today; kept defensively)
        default: 'h-[38px] px-4 text-sm',
      },
    },
    defaultVariants: { variant: 'primary', size: 'md' },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button';
    return (
      <Comp
        ref={ref}
        className={cn(buttonVariants({ variant, size, className }))}
        {...props}
      />
    );
  }
);
Button.displayName = 'Button';

export { Button, buttonVariants };
