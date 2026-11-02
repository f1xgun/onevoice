import { formatClockTime } from './time-display';

type Translate = (key: string, values?: Record<string, string | number>) => string;

export function decisionDay(value: string, now = new Date()) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return null;
  const day = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  const tomorrow = new Date(today);
  tomorrow.setDate(today.getDate() + 1);
  return {
    date,
    kind:
      day === today.getTime()
        ? 'today'
        : day === yesterday.getTime()
          ? 'yesterday'
          : day === tomorrow.getTime()
            ? 'tomorrow'
            : 'date',
  };
}

export function decisionTimestamp(value: string, locale: string, t: Translate, prose = false) {
  const day = decisionDay(value);
  if (!day) return '';
  return t(`${prose ? 'whenProse' : 'when'}.${day.kind}`, {
    time: formatClockTime(day.date),
    date: new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'long' }).format(day.date),
  });
}

const yandexDays = ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday'];
const days = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;

function readSchedule(value: string | undefined) {
  if (!value) return null;
  try {
    const parsed: unknown = JSON.parse(value);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

function scheduleValue(value: unknown) {
  if (value === 'closed') return { closed: true, open: '', close: '' };
  if (!value || typeof value !== 'object') return null;
  const hours = value as Record<string, unknown>;
  if (
    typeof hours.open !== 'string' ||
    typeof hours.close !== 'string' ||
    !/^\d{2}:\d{2}$/.test(hours.open) ||
    !/^\d{2}:\d{2}$/.test(hours.close)
  )
    return null;
  return { closed: false, open: formatClockTime(hours.open), close: formatClockTime(hours.close) };
}

export function firstScheduleDifference(remoteValue?: string, storedValue?: string) {
  const remote = readSchedule(remoteValue),
    stored = readSchedule(storedValue);
  if (!remote || !stored) return null;
  for (const [index, key] of yandexDays.entries()) {
    const before = scheduleValue(remote[key]),
      after = scheduleValue(stored[key]);
    if (before && after && JSON.stringify(before) !== JSON.stringify(after))
      return { day: days[index], remote: before, stored: after };
  }
  return null;
}
