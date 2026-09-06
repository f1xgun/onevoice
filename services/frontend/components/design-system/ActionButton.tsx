'use client';

import { forwardRef } from 'react';
import type { ButtonProps } from '@/components/ui/button';
import { Button } from '@/components/ui/button';
import { actionButtonVariants } from './action-button-variants';

export type ActionButtonProps = ButtonProps;

export const ActionButton = forwardRef<HTMLButtonElement, ActionButtonProps>(function ActionButton(
  { variant, size, className, disabled, asChild, onClick, onClickCapture, ...props },
  ref
) {
  return (
    <Button
      {...props}
      ref={ref}
      asChild={asChild}
      variant={variant}
      size={size}
      disabled={disabled}
      aria-disabled={disabled || props['aria-disabled']}
      tabIndex={asChild && disabled ? -1 : props.tabIndex}
      onClick={onClick}
      onClickCapture={
        disabled
          ? function preventDisabledAction(event) {
              event.preventDefault();
              event.stopPropagation();
            }
          : onClickCapture
      }
      data-ov-motion
      className={actionButtonVariants({ variant, size, className })}
    />
  );
});
