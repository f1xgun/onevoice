// Locale primitives shared by the resolver (`request.ts`), the cookie
// route handler (`app/api/locale/route.ts`), the `<LanguageSwitcher>`
// UI, and the axios interceptor that forwards the current locale to the
// backend as `Accept-Language`. Keeping these constants in one tiny
// module avoids a circular import between `request.ts` (server-only
// `next/headers` import) and call-sites that ship to the client bundle.

export const SUPPORTED_LOCALES = ['ru', 'en'] as const;

export type Locale = (typeof SUPPORTED_LOCALES)[number];

export const DEFAULT_LOCALE: Locale = 'ru';

// Cookie name is `NEXT_LOCALE` to align with next-intl's convention
// (and to stay forward-compatible with the official middleware-based
// router setup if we ever adopt it).
export const LOCALE_COOKIE = 'NEXT_LOCALE';

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}
