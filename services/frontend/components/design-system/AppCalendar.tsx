'use client';

import { useEffect, useRef } from 'react';
import type { ComponentProps } from 'react';
import type { DayButton } from 'react-day-picker';
import { Calendar } from '@/components/ui/calendar';
import { cn } from '@/lib/utils';
import { ActionButton } from './ActionButton';
import { actionButtonVariants } from './action-button-variants';

function AppDayButton({ day, modifiers, className, ...props }: ComponentProps<typeof DayButton>) {
  const ref = useRef<HTMLButtonElement>(null);
  useEffect(
    function focusDay() {
      if (modifiers.focused) ref.current?.focus();
    },
    [modifiers.focused]
  );
  return (
    <ActionButton
      {...props}
      ref={ref}
      variant={modifiers.selected ? 'primary' : 'secondary'}
      size="icon"
      aria-pressed={Boolean(modifiers.selected)}
      className={cn(
        'w-full font-normal aria-pressed:font-semibold aria-pressed:underline',
        className
      )}
      data-day={day.date.toLocaleDateString()}
    />
  );
}

export function AppCalendar({
  className,
  classNames,
  components,
  ...props
}: ComponentProps<typeof Calendar>) {
  return (
    <Calendar
      {...props}
      data-ov-motion
      className={cn('max-w-full p-0 [--cell-size:2.75rem]', className)}
      classNames={{
        ...classNames,
        button_previous: actionButtonVariants({
          variant: 'secondary',
          size: 'icon',
          className: classNames?.button_previous,
        }),
        button_next: actionButtonVariants({
          variant: 'secondary',
          size: 'icon',
          className: classNames?.button_next,
        }),
        disabled: 'text-ink-soft',
      }}
      components={{ DayButton: AppDayButton, ...components }}
    />
  );
}
