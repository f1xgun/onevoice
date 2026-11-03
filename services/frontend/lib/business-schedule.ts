import type { ScheduleDay, SpecialDate } from '@/types/business';
import { formatClockTime } from './time-display';

export const WEEK_DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;

export function openingHours(
  day: { closed: boolean; open?: string; close?: string },
  dayOff: string
) {
  if (day.closed) return dayOff;
  const open = day.open && formatClockTime(day.open),
    close = day.close && formatClockTime(day.close);
  return open && close ? `${open}–${close}` : '';
}

export function groupOpeningHours(schedule: ScheduleDay[]) {
  const groups: { days: ScheduleDay['day'][]; hours: ScheduleDay }[] = [];
  for (const [index, key] of WEEK_DAYS.entries()) {
    const day = schedule.find((entry) => entry.day === key);
    if (!day) continue;
    const last = groups[groups.length - 1];
    if (
      last &&
      last.days[last.days.length - 1] === WEEK_DAYS[index - 1] &&
      last.hours.closed === day.closed &&
      (day.closed || (last.hours.open === day.open && last.hours.close === day.close))
    )
      last.days.push(key);
    else groups.push({ days: [key], hours: day });
  }
  return groups;
}

export function effectiveHours(
  schedule: ScheduleDay[],
  specialDates: SpecialDate[],
  now = new Date()
) {
  const date = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  const special = specialDates.find((entry) => entry.date === date);
  if (special) return special;
  const day = (['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const)[now.getDay()];
  return schedule.find((entry) => entry.day === day);
}
