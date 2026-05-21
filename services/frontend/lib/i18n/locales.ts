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

// RFC 9110-style Accept-Language parser. We honour q-factors so a client
// sending `ru;q=0.1, en;q=0.9` correctly resolves to `en` (the previous
// in-order parser would have picked `ru`). Entries are sorted by quality
// descending (stable for equal q to preserve document order) and the
// first supported base tag wins. Falls back to `DEFAULT_LOCALE` when the
// header is empty, malformed, or names only unsupported languages.
const DEFAULT_QUALITY = 1.0;
const MIN_QUALITY = 0.0;
const MAX_QUALITY = 1.0;

interface ParsedEntry {
  base: string;
  q: number;
  index: number;
}

function parseQuality(raw: string | undefined): number | null {
  if (raw === undefined) return DEFAULT_QUALITY;
  const trimmed = raw.trim();
  if (!trimmed.toLowerCase().startsWith('q=')) return null;
  const value = Number.parseFloat(trimmed.slice(2));
  if (!Number.isFinite(value) || value < MIN_QUALITY || value > MAX_QUALITY) return null;
  return value;
}

export function parseAcceptLanguage(header: string): Locale {
  if (!header) return DEFAULT_LOCALE;

  const entries: ParsedEntry[] = [];
  const rawEntries = header.split(',');
  for (let index = 0; index < rawEntries.length; index++) {
    const parts = rawEntries[index].split(';');
    const tag = parts[0]?.trim().toLowerCase();
    if (!tag) continue;
    // Validate language tag shape: letters and dashes only, plus an
    // optional `*` wildcard which we treat as the default fallback.
    if (!/^[a-z*]+(-[a-z0-9]+)*$/i.test(tag)) continue;
    const q = parseQuality(parts[1]);
    if (q === null || q === 0) continue;
    const base = tag.split('-')[0];
    entries.push({ base, q, index });
  }

  entries.sort((a, b) => (b.q !== a.q ? b.q - a.q : a.index - b.index));

  for (const entry of entries) {
    if ((SUPPORTED_LOCALES as readonly string[]).includes(entry.base)) {
      return entry.base as Locale;
    }
  }

  return DEFAULT_LOCALE;
}
