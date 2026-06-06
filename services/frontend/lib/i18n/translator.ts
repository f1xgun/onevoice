// Server-side translator helper for non-React contexts.
// React components MUST use `useTranslations` from `next-intl` directly.

import { createTranslator } from 'next-intl';
import { getLocale } from 'next-intl/server';
import type { NamespaceKeys, NestedKeyOf } from 'next-intl';
import type ruMessagesType from '@/messages/ru.json';
import { DEFAULT_LOCALE, isLocale, type Locale } from '@/lib/i18n/locales';

// Type-only import keeps next-intl `NamespaceKeys` validation intact
// without dragging the JSON bundle into client code.
type Messages = typeof ruMessagesType;
type Namespaces = NamespaceKeys<Messages, NestedKeyOf<Messages>>;

async function loadMessages(locale: Locale): Promise<Messages> {
  const mod = await import(`@/messages/${locale}.json`);
  return (mod.default ?? mod) as Messages;
}

/**
 * Async translator for non-React server-side contexts. If `locale` is
 * omitted, falls back to `getLocale()` from `next-intl/server`.
 */
export async function getServerTranslator<N extends Namespaces>(namespace: N, locale?: Locale) {
  const resolvedLocale: Locale = locale ?? (await resolveServerLocale());
  const messages = await loadMessages(resolvedLocale);
  return createTranslator({ locale: resolvedLocale, messages, namespace });
}

async function resolveServerLocale(): Promise<Locale> {
  try {
    const detected = await getLocale();
    if (isLocale(detected)) return detected;
  } catch {}
  return DEFAULT_LOCALE;
}
