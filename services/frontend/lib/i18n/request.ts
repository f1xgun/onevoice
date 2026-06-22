import { cookies, headers } from 'next/headers';
import { getRequestConfig } from 'next-intl/server';
import { isLocale, LOCALE_COOKIE, parseAcceptLanguage, type Locale } from './locales';
import { intlMessageFallback, onIntlError } from './fallback';

// next-intl request config. Locale resolution precedence (highest → lowest):
//   1. `NEXT_LOCALE` cookie (set by POST /api/locale via <LanguageSwitcher>).
//   2. `Accept-Language` header — RFC 9110 q-factor-aware (see
//      `parseAcceptLanguage` in `./locales`).
//   3. `DEFAULT_LOCALE` ('ru').

async function resolveLocale(): Promise<Locale> {
  const cookieStore = await cookies();
  const fromCookie = cookieStore.get(LOCALE_COOKIE)?.value;
  if (isLocale(fromCookie)) {
    return fromCookie;
  }

  const headerStore = await headers();
  const accept = headerStore.get('accept-language') ?? '';
  return parseAcceptLanguage(accept);
}

export default getRequestConfig(async () => {
  const locale = await resolveLocale();
  const messages = (await import(`../../messages/${locale}.json`)).default;
  return {
    locale,
    messages,
    onError: onIntlError,
    getMessageFallback: intlMessageFallback,
  };
});
