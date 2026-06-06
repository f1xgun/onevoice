import { describe, expect, it } from 'vitest';

import { DEFAULT_LOCALE, parseAcceptLanguage } from '../locales';

// RFC 9110 q-factor-aware Accept-Language resolution. The parser lives
// next to the locale primitives so both the server resolver
// (`lib/i18n/request.ts`) and any future call-site share one source of
// truth. Each case below targets a documented regression or edge case.
describe('parseAcceptLanguage', () => {
  it("resolves a plain supported base tag ('en') to itself", () => {
    expect(parseAcceptLanguage('en')).toBe('en');
  });

  it("normalises a region subtag ('ru-RU') to its base language", () => {
    expect(parseAcceptLanguage('ru-RU')).toBe('ru');
  });

  it("falls back to DEFAULT_LOCALE when no supported tag is present ('fr')", () => {
    expect(parseAcceptLanguage('fr')).toBe(DEFAULT_LOCALE);
  });

  it('respects positive q-factors when document order would lose (en-US;q=0.9, ru;q=0.5)', () => {
    expect(parseAcceptLanguage('en-US;q=0.9, ru;q=0.5')).toBe('en');
  });

  it('picks the higher-q tag even when the lower-q tag is listed first (ru;q=0.1, en;q=0.9)', () => {
    expect(parseAcceptLanguage('ru;q=0.1, en;q=0.9')).toBe('en');
  });

  it('falls back to DEFAULT_LOCALE on an empty header', () => {
    expect(parseAcceptLanguage('')).toBe(DEFAULT_LOCALE);
  });

  it('falls back to DEFAULT_LOCALE on garbage input', () => {
    expect(parseAcceptLanguage('!!garbage!!')).toBe(DEFAULT_LOCALE);
  });

  it('preserves document order for equal q-values (ru, en → ru)', () => {
    expect(parseAcceptLanguage('ru, en')).toBe('ru');
  });
});
