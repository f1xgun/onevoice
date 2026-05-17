// Translator helpers for non-React contexts.
//
// React components MUST use `useTranslations` from `next-intl` directly.
// Two helpers exist here for code paths where hooks aren't available:
//
//   1. `getServerTranslator(namespace, locale?)` — async, server-only.
//      Resolves the current locale from `next-intl/server.getLocale()`
//      when omitted, dynamically imports the matching messages bundle,
//      and returns a `createTranslator(...)` instance. Use this from
//      Server Components, route handlers, and other server-side helpers.
//
//   2. `getTranslator(namespace)` — DEPRECATED synchronous shim.
//      Kept as a backwards-compatibility bridge during the Phase B1
//      migration of every module-level consumer to a request-scoped
//      pattern (see `.planning/i18n-readiness/PLAN.md`, B1). It pins
//      the locale to `ru` at module-load time, which BREAKS the runtime
//      locale switch from Phase A2 for any string it produces. This
//      export will be removed in the final B1 commit once all in-tree
//      callers have migrated to `useTranslations` (React), the factory
//      pattern + `useMemo`, or `getServerTranslator` (server).
//
// Both helpers are typed against next-intl's `NamespaceKeys` / `NestedKeyOf`
// so the same key-validation `useTranslations` enforces flows through to
// the call sites here.

import { createTranslator } from 'next-intl';
import { getLocale } from 'next-intl/server';
import type { NamespaceKeys, NestedKeyOf } from 'next-intl';
import ruMessages from '@/messages/ru.json';
import { DEFAULT_LOCALE, isLocale, type Locale } from '@/lib/i18n/locales';

type Messages = typeof ruMessages;
type Namespaces = NamespaceKeys<Messages, NestedKeyOf<Messages>>;

// `import()` returns a Module record; we read `.default` to land on the
// JSON's actual shape (typeof ruMessages). The fetcher accepts a Locale
// so future callers can opt-out of cookie-driven resolution.
async function loadMessages(locale: Locale): Promise<Messages> {
  // The webpack/turbopack JSON loader resolves these as ESM with `default`.
  const mod = await import(`@/messages/${locale}.json`);
  return (mod.default ?? mod) as Messages;
}

/**
 * Async translator for non-React server-side contexts (route handlers,
 * server components that aren't already using `getTranslations`, and any
 * helper that needs request-scoped strings).
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

/**
 * @deprecated Use `useTranslations` (React tree) or `getServerTranslator`
 * (server, async). This synchronous helper is pinned to the `ru` bundle
 * at module-load time and CANNOT honor the runtime locale switch from
 * Phase A2. Remaining call sites are being migrated in Phase B1; once
 * the last one is gone, this export will be deleted.
 */
export function getTranslator<N extends Namespaces>(namespace: N) {
  return createTranslator({ locale: DEFAULT_LOCALE, messages: ruMessages, namespace });
}
