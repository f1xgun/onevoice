import { getRequestConfig } from 'next-intl/server';

// next-intl request config — pinned to a single locale ("ru") for the
// MVP. There's no [locale] route segment and no middleware-based
// negotiation: every request resolves to the same message bundle. When
// English support lands, this is the only place that needs to learn
// about runtime negotiation (cookie, accept-language, etc.).
export const DEFAULT_LOCALE = 'ru';

export default getRequestConfig(async () => {
  const messages = (await import(`../../messages/${DEFAULT_LOCALE}.json`)).default;
  return {
    locale: DEFAULT_LOCALE,
    messages,
  };
});
