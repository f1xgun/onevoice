import { cookies, headers } from 'next/headers';
import { getRequestConfig } from 'next-intl/server';
import { DEFAULT_LOCALE, isLocale, LOCALE_COOKIE, SUPPORTED_LOCALES, type Locale } from './locales';

// next-intl request config. Locale resolution precedence (highest → lowest):
//   1. `NEXT_LOCALE` cookie (set by POST /api/locale via <LanguageSwitcher>).
//   2. `Accept-Language` header (base language tag matches a supported locale).
//   3. `DEFAULT_LOCALE` ('ru').
//
// Kept deliberately small — no negotiator library. With two locales the
// header-parsing rule is just "first quality-ordered base tag that we
// support". Phase A1 plans to add the same precedence on the Go backend
// (`pkg/i18n`) so cookie + header agree on both sides.

function parseAcceptLanguage(header: string): Locale | undefined {
  // Each entry: "ru-RU;q=0.9". We strip the `;q=...` suffix, take the
  // base language (`ru-RU` → `ru`), and pick the first supported tag.
  // Order in the header already implies quality; we don't re-sort because
  // implementations that omit `q` rely on positional order.
  for (const raw of header.split(',')) {
    const tag = raw.split(';')[0]?.trim().toLowerCase();
    if (!tag) continue;
    const base = tag.split('-')[0];
    if ((SUPPORTED_LOCALES as readonly string[]).includes(base)) {
      return base as Locale;
    }
  }
  return undefined;
}

async function resolveLocale(): Promise<Locale> {
  const cookieStore = await cookies();
  const fromCookie = cookieStore.get(LOCALE_COOKIE)?.value;
  if (isLocale(fromCookie)) {
    return fromCookie;
  }

  const headerStore = await headers();
  const accept = headerStore.get('accept-language') ?? '';
  const fromHeader = parseAcceptLanguage(accept);
  if (fromHeader) {
    return fromHeader;
  }

  return DEFAULT_LOCALE;
}

export default getRequestConfig(async () => {
  const locale = await resolveLocale();
  const messages = (await import(`../../messages/${locale}.json`)).default;
  return {
    locale,
    messages,
  };
});
