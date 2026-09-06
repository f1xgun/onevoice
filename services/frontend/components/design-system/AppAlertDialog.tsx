'use client';

import { forwardRef } from 'react';
import { Content } from '@radix-ui/react-alert-dialog';
import { AlertDialogPortal, AlertDialogOverlay } from '@/components/ui/alert-dialog';
import type { ButtonProps } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import type { ComponentPropsWithoutRef, ElementRef } from 'react';
import {
  AlertDialogAction as Action,
  AlertDialogCancel as Cancel,
} from '@/components/ui/alert-dialog';
import { actionButtonVariants } from './action-button-variants';

export {
  AlertDialog,
  AlertDialogTrigger,
  AlertDialogHeader,
  AlertDialogFooter,
  AlertDialogTitle,
  AlertDialogDescription,
} from '@/components/ui/alert-dialog';

export const AlertDialogAction = forwardRef<
  ElementRef<typeof Action>,
  ComponentPropsWithoutRef<typeof Action> & { variant?: ButtonProps['variant'] }
>(function AlertDialogAction({ className, variant = 'primary', ...props }, ref) {
  return <Action {...props} ref={ref} className={actionButtonVariants({ variant, className })} />;
});

export const AlertDialogCancel = forwardRef<
  ElementRef<typeof Cancel>,
  ComponentPropsWithoutRef<typeof Cancel>
>(function AlertDialogCancel({ className, ...props }, ref) {
  return (
    <Cancel
      {...props}
      ref={ref}
      className={actionButtonVariants({ variant: 'secondary', className })}
    />
  );
});

export const AlertDialogContent = forwardRef<
  ElementRef<typeof Content>,
  ComponentPropsWithoutRef<typeof Content>
>(function AlertDialogContent({ className, ...props }, ref) {
  return (
    <AlertDialogPortal>
      <AlertDialogOverlay data-ov-motion className="bg-overlay duration-150" />
      <Content
        {...props}
        ref={ref}
        data-ov-motion
        className={cn(
          className,
          'fixed left-1/2 top-1/2 z-50 grid max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] max-w-[30rem] -translate-x-1/2 -translate-y-1/2 gap-4 overflow-y-auto rounded-xl border border-control bg-card p-4 text-ink shadow-overlay sm:p-6'
        )}
      />
    </AlertDialogPortal>
  );
});
