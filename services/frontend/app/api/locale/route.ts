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

// Lightweight in-memory token-bucket rate limiter (30 req / minute per IP).
// Dependency-free and intentionally per-instance — the route only writes a
// UI-preference cookie, so eventual consistency across replicas is fine and
// global rate-limit infra would be overkill. The bucket map is bounded by
// natural churn: stale entries are pruned lazily inside `consume`.
const BUCKET_CAPACITY = 30;
const REFILL_WINDOW_MS = 60_000;
const REFILL_PER_MS = BUCKET_CAPACITY / REFILL_WINDOW_MS;
// Drop bucket entries idle for > 2x the refill window to bound memory.
const BUCKET_TTL_MS = REFILL_WINDOW_MS * 2;
// Threshold above which we sweep idle entries on the next `consume` call.
const BUCKET_PRUNE_THRESHOLD = 1024;

type Bucket = { tokens: number; lastRefillMs: number };
const buckets = new Map<string, Bucket>();

function consume(ip: string, nowMs: number): boolean {
  if (buckets.size > BUCKET_PRUNE_THRESHOLD) {
    for (const [key, bucket] of buckets) {
      if (nowMs - bucket.lastRefillMs > BUCKET_TTL_MS) buckets.delete(key);
    }
  }

  const existing = buckets.get(ip);
  if (!existing) {
    buckets.set(ip, { tokens: BUCKET_CAPACITY - 1, lastRefillMs: nowMs });
    return true;
  }

  const elapsed = Math.max(0, nowMs - existing.lastRefillMs);
  const refilled = Math.min(BUCKET_CAPACITY, existing.tokens + elapsed * REFILL_PER_MS);
  if (refilled < 1) {
    existing.tokens = refilled;
    existing.lastRefillMs = nowMs;
    return false;
  }
  existing.tokens = refilled - 1;
  existing.lastRefillMs = nowMs;
  return true;
}

function clientIp(request: Request): string {
  const forwarded = request.headers.get('x-forwarded-for');
  if (forwarded) {
    const first = forwarded.split(',')[0]?.trim();
    if (first) return first;
  }
  return request.headers.get('x-real-ip')?.trim() || 'unknown';
}

export async function POST(request: Request): Promise<Response> {
  if (!consume(clientIp(request), Date.now())) {
    return new Response(JSON.stringify({ error: 'rate limited' }), {
      status: 429,
      headers: { 'Content-Type': 'application/json' },
    });
  }

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
