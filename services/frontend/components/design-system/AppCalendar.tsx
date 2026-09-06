'use client';

import { useEffect, useRef } from 'react';
import type { ComponentProps } from 'react';
import type { DayButton } from 'react-day-picker';
import { Calendar } from '@/components/ui/calendar';
import { cn } from '@/lib/utils';
import { ActionButton } from './ActionButton';
import { actionButtonVariants } from './action-button-variants';

interface AppDayButtonProps extends ComponentProps<typeof DayButton> {
  className?: string;
}

function AppDayButton({ day, modifiers, className, ...props }: AppDayButtonProps) {
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

type CalendarProps = ComponentProps<typeof Calendar>;

export interface AppCalendarSingleProps extends Extract<
  CalendarProps,
  { mode: 'single'; required?: false }
> {
  mode: 'single';
}
export interface AppCalendarSingleRequiredProps extends Extract<
  CalendarProps,
  { mode: 'single'; required: true }
> {
  mode: 'single';
}
export interface AppCalendarMultipleProps extends Extract<
  CalendarProps,
  { mode: 'multiple'; required?: false }
> {
  mode: 'multiple';
}
export interface AppCalendarMultipleRequiredProps extends Extract<
  CalendarProps,
  { mode: 'multiple'; required: true }
> {
  mode: 'multiple';
}
export interface AppCalendarRangeProps extends Extract<
  CalendarProps,
  { mode: 'range'; required?: false }
> {
  mode: 'range';
}
export interface AppCalendarRangeRequiredProps extends Extract<
  CalendarProps,
  { mode: 'range'; required: true }
> {
  mode: 'range';
}
export interface AppCalendarUnselectedProps extends Extract<CalendarProps, { mode?: undefined }> {
  mode?: undefined;
}

export type AppCalendarProps =
  | AppCalendarSingleProps
  | AppCalendarSingleRequiredProps
  | AppCalendarMultipleProps
  | AppCalendarMultipleRequiredProps
  | AppCalendarRangeProps
  | AppCalendarRangeRequiredProps
  | AppCalendarUnselectedProps;

export function AppCalendar({ className, classNames, components, ...props }: AppCalendarProps) {
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
