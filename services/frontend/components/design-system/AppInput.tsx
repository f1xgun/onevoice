'use client';

import { forwardRef } from 'react';
import type { ComponentProps } from 'react';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';

const fieldClasses =
  'min-h-11 h-auto rounded-md border-control bg-paper-raised text-reading text-ink shadow-none focus-visible:border-brand focus-visible:ring-brand focus-visible:ring-offset-2 disabled:bg-paper-sunken disabled:text-ink-soft disabled:opacity-100';

export const AppInput = forwardRef<HTMLInputElement, ComponentProps<typeof Input>>(
  function AppInput({ className, ...props }, ref) {
    return <Input {...props} ref={ref} className={cn(className, fieldClasses)} />;
  }
);

export const AppTextarea = forwardRef<HTMLTextAreaElement, ComponentProps<typeof Textarea>>(
  function AppTextarea({ className, ...props }, ref) {
    return (
      <Textarea {...props} ref={ref} className={cn(className, fieldClasses, 'md:text-reading')} />
    );
  }
);
