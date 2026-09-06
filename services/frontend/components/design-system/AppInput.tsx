'use client';

import { forwardRef } from 'react';
import type { ComponentPropsWithoutRef } from 'react';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';

const fieldClasses =
  'min-h-11 h-auto rounded-md border-control bg-paper-raised text-reading text-ink shadow-none focus-visible:border-brand focus-visible:ring-brand focus-visible:ring-offset-2 disabled:bg-paper-sunken disabled:text-ink-soft disabled:opacity-100';

export interface AppInputProps extends ComponentPropsWithoutRef<typeof Input> {
  className?: string;
}

export interface AppTextareaProps extends ComponentPropsWithoutRef<typeof Textarea> {
  className?: string;
}

export const AppInput = forwardRef<HTMLInputElement, AppInputProps>(function AppInput(
  { className, ...props },
  ref
) {
  return <Input {...props} ref={ref} className={cn(className, fieldClasses)} />;
});

export const AppTextarea = forwardRef<HTMLTextAreaElement, AppTextareaProps>(function AppTextarea(
  { className, ...props },
  ref
) {
  return (
    <Textarea {...props} ref={ref} className={cn(className, fieldClasses, 'md:text-reading')} />
  );
});
