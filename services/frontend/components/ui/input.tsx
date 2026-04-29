// components/ui/input.tsx — OneVoice (Linen) override

import * as React from 'react';
import { cn } from '@/lib/utils';

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  mono?: boolean;
}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, mono, ...props }, ref) => {
    return (
      <input
        type={type}
        ref={ref}
        className={cn(
          'flex h-10 w-full rounded-md',
          'border border-[var(--ov-line)] bg-[var(--ov-paper-raised)]',
          'px-3 py-2 text-sm text-[var(--ov-ink)]',
          'placeholder:text-[var(--ov-ink-soft)]',
          'transition-[border-color,box-shadow] duration-150',
          'focus:outline-none focus:border-[var(--ov-accent)] focus:ring-2 focus:ring-[var(--ov-accent)]/20',
          'disabled:cursor-not-allowed disabled:opacity-50',
          'file:border-0 file:bg-transparent file:text-sm file:font-medium',
          mono && 'font-mono tracking-[0]',
          className
        )}
        {...props}
      />
    );
  }
);
Input.displayName = 'Input';

export { Input };
