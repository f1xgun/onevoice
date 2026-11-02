/** Compact 24-hour time used in owner-facing summaries. */
export function formatClockTime(value: string | Date): string {
  if (typeof value === 'string' && /^\d{1,2}:\d{2}(?::\d{2})?$/.test(value)) {
    return value
      .split(':')
      .slice(0, 2)
      .join(':')
      .replace(/^0(?=\d:)/, '');
  }
  const date = value instanceof Date ? value : new Date(value);
  if (!Number.isFinite(date.getTime())) return '';
  return `${date.getHours()}:${String(date.getMinutes()).padStart(2, '0')}`;
}
