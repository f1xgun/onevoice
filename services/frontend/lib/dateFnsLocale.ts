// lib/dateFnsLocale.ts — runtime locale binding for date-fns.
//
// Replaces hardcoded `import { ru } from 'date-fns/locale'` sites so a
// language switch via next-intl flips date formatting without remount.
// Subpath imports (`date-fns/locale/ru`, `date-fns/locale/en-US`) keep
// the bundle slim — pulling the barrel `date-fns/locale` would ship every
// supported locale.

import { ru } from 'date-fns/locale/ru';
import { enUS } from 'date-fns/locale/en-US';
import type { Locale } from '@/lib/i18n/locales';

const map = { ru, en: enUS } as const;

export function getDateFnsLocale(locale: Locale) {
  return map[locale];
}
