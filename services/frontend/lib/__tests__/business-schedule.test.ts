import { expect, it } from 'vitest';
import { effectiveHours, groupOpeningHours, openingHours, WEEK_DAYS } from '../business-schedule';
const schedule = WEEK_DAYS.map((day, index) => ({
  day,
  open: index < 5 ? '08:00' : '09:00',
  close: '21:00',
  closed: false,
}));
it('groups the actual weekday and weekend intervals', () => {
  const groups = groupOpeningHours(schedule);
  expect(groups.map((group) => group.days)).toEqual([
    ['mon', 'tue', 'wed', 'thu', 'fri'],
    ['sat', 'sun'],
  ]);
  expect(openingHours(groups[0].hours, 'Выходной')).toBe('8:00–21:00');
});
it('uses a closed special date over the weekly hours for today', () => {
  const day = effectiveHours(
    schedule,
    [{ date: '2026-11-04', closed: true }],
    new Date(2026, 10, 4, 12)
  );
  expect(day?.closed).toBe(true);
  expect(openingHours(day!, 'Выходной')).toBe('Выходной');
  expect(effectiveHours(schedule, [], new Date(2026, 10, 3, 12))).toMatchObject({ day: 'tue' });
});
it('does not merge equal intervals across an unrecorded day', () => {
  const groups = groupOpeningHours([schedule[0], schedule[2]]);
  expect(groups).toHaveLength(2);
});
