// Server-side translator helper for non-React contexts.
//
// React components MUST use `useTranslations` from `next-intl` directly.
// `getServerTranslator(namespace, locale?)` exists for code paths where
// hooks aren't available: route handlers, server components that aren't
// already using `getTranslations`, and any helper that needs
// request-scoped strings.
//
// Locale resolution: if `locale` is omitted the function calls
// `getLocale()` from `next-intl/server`, which reads the request config
// from `lib/i18n/request.ts` (cookie → Accept-Language → default).
//
// History: the previous module-level `getTranslator(ns)` shim — which
// pinned ru.json + hardcoded `LOCALE = 'ru'` at module load and broke
// the Phase A2 runtime locale switch — was removed in Phase B1 after
// all in-tree consumers migrated. The static `ru.json` import is gone;
// nothing in this module ships locale-bound strings into the client
// bundle anymore.

import { createTranslator } from 'next-intl';
import { getLocale } from 'next-intl/server';
import type { NamespaceKeys, NestedKeyOf } from 'next-intl';
import type ruMessagesType from '@/messages/ru.json';
import { DEFAULT_LOCALE, isLocale, type Locale } from '@/lib/i18n/locales';

// Type-only import of `ru.json` keeps the next-intl `NamespaceKeys`
// type-validation intact without dragging the bundle into client code.
type Messages = typeof ruMessagesType;
type Namespaces = NamespaceKeys<Messages, NestedKeyOf<Messages>>;

async function loadMessages(locale: Locale): Promise<Messages> {
  // The webpack/turbopack JSON loader resolves these as ESM with `default`.
  const mod = await import(`@/messages/${locale}.json`);
  return (mod.default ?? mod) as Messages;
}

/**
 * Async translator for non-React server-side contexts (route handlers,
 * server components that aren't already using `getTranslations`, and
 * any helper that needs request-scoped strings).
 *
 * If `locale` is omitted the function calls `getLocale()` from
 * `next-intl/server`, which reads the request config from
 * `lib/i18n/request.ts` (cookie → Accept-Language → default).
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
  } catch {
    // `getLocale()` throws when called outside a request scope (e.g.
    // during static analysis or a test harness). Fall through to the
    // default locale rather than crashing.
  }
  return DEFAULT_LOCALE;
}
