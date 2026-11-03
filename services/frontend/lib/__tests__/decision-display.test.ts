import { expect, it } from 'vitest';
import { decisionDay, firstScheduleDifference } from '../decision-display';

it('compares calendar days and omits invalid dates', () => {
  const now = new Date(2026, 10, 3, 12);
  expect(decisionDay(new Date(2026, 10, 2, 21, 14).toISOString(), now)?.kind).toBe('yesterday');
  expect(decisionDay(new Date(2026, 10, 4, 9).toISOString(), now)?.kind).toBe('tomorrow');
  expect(decisionDay('bad date', now)).toBeNull();
});

it('uses the real schedule values and strips leading zeroes in the hours difference', () => {
  const remote = JSON.stringify({ saturday: { open: '10:00', close: '21:00' } });
  const stored = JSON.stringify({ saturday: { open: '09:00', close: '21:00' } });
  expect(firstScheduleDifference(remote, stored)).toEqual({
    day: 'sat',
    remote: { closed: false, open: '10:00', close: '21:00' },
    stored: { closed: false, open: '9:00', close: '21:00' },
  });
  expect(firstScheduleDifference(remote, remote)).toBeNull();
  expect(firstScheduleDifference('broken', stored)).toBeNull();
});
