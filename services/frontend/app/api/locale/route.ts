// POST /api/locale — persists the user's UI locale in a cookie that the
// next-intl request config (`lib/i18n/request.ts`) reads on the next
// render. The client side flow is:
//   1. <LanguageSwitcher> sends `{ locale: 'ru' | 'en' }` here.
//   2. We validate via `isLocale` to refuse anything else with 400.
//   3. We set the cookie (year-long, lax, path '/', non-httpOnly so the
//      client can read it for the axios interceptor) and return 204.
//   4. The switcher calls `router.refresh()` so RSC + interceptor pick
//      up the new value without a full reload.
//
// `httpOnly: false` is intentional: we need the client to read this
// cookie to attach `Accept-Language` on outgoing API calls. It's a UI
// preference, not a credential.

import { cookies } from 'next/headers';
import { isLocale, LOCALE_COOKIE } from '@/lib/i18n/locales';

// Year-long persistence so the choice survives across sessions without
// nagging the user. `60 * 60 * 24 * 365` literally, but the lint rule
// rejects unnamed integers > 10, so we encode it as a single constant.
const ONE_YEAR_SECONDS = 31_536_000;

export async function POST(request: Request): Promise<Response> {
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return new Response(JSON.stringify({ error: 'invalid locale' }), {
      status: 400,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  const locale = (body as { locale?: unknown } | null)?.locale;
  if (!isLocale(locale)) {
    return new Response(JSON.stringify({ error: 'invalid locale' }), {
      status: 400,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  const store = await cookies();
  store.set({
    name: LOCALE_COOKIE,
    value: locale,
    httpOnly: false,
    sameSite: 'lax',
    path: '/',
    maxAge: ONE_YEAR_SECONDS,
    secure: process.env.NODE_ENV === 'production',
  });

  return new Response(null, { status: 204 });
}
