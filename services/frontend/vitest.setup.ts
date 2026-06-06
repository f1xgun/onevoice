import '@testing-library/jest-dom';
import { afterEach, vi } from 'vitest';

// Global next-intl stub. Looks keys up in messages/{ru,en}.json so tests
// can assert literal copy without mounting NextIntlClientProvider. Missing
// keys fall back to the namespaced key string for clearer failure surfaces.
//
// DEFAULT LOCALE IS `ru` — many component tests assert RU literals directly.
// Tests exercising EN opt in via `__setTestLocale('en')`; the global
// afterEach below resets back to `ru`.
import ruMessages from './messages/ru.json';
import enMessages from './messages/en.json';

type SupportedLocale = 'ru' | 'en';
const messageBundles: Record<SupportedLocale, unknown> = {
  ru: ruMessages,
  en: enMessages,
};

const state: { locale: SupportedLocale } = { locale: 'ru' };

// Per-test locale override: `__setTestLocale('en')` switches the lookup
// bundle; the global afterEach resets to 'ru'.
function setTestLocale(locale: SupportedLocale): void {
  state.locale = locale;
}
(globalThis as unknown as { __setTestLocale: typeof setTestLocale }).__setTestLocale =
  setTestLocale;

afterEach(() => {
  state.locale = 'ru';
});

function lookupRaw(namespace: string | undefined, key: string): unknown {
  const path = namespace ? `${namespace}.${key}`.split('.') : key.split('.');
  let cursor: unknown = messageBundles[state.locale];
  for (const part of path) {
    if (typeof cursor === 'object' && cursor !== null && part in cursor) {
      cursor = (cursor as Record<string, unknown>)[part];
    } else {
      return undefined;
    }
  }
  return cursor;
}

function lookupTranslation(namespace: string | undefined, key: string): string {
  const v = lookupRaw(namespace, key);
  return typeof v === 'string' ? v : namespace ? `${namespace}.${key}` : key;
}

// Russian CLDR plural category for an integer. Hand-rolled instead of
// using Intl.PluralRules so the mock doesn't pull in all of next-intl.
function ruPluralCategory(n: number): 'one' | 'few' | 'many' | 'other' {
  if (!Number.isInteger(n)) return 'other';
  const mod10 = Math.abs(n) % 10;
  const mod100 = Math.abs(n) % 100;
  if (mod10 === 1 && mod100 !== 11) return 'one';
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return 'few';
  return 'many';
}

// Extract a braced body starting after the opening `{`. Tracks brace
// depth so nested option bodies don't terminate the outer block early.
function extractPluralBody(src: string, start: number): { body: string; end: number } | null {
  let depth = 1;
  let i = start;
  while (i < src.length && depth > 0) {
    const ch = src[i];
    if (ch === '{') depth++;
    else if (ch === '}') depth--;
    if (depth === 0) return { body: src.slice(start, i), end: i };
    i++;
  }
  return null;
}

// Parse option list inside an ICU plural body: `one {…} few {…} =0 {…}`.
function parsePluralOptions(body: string): Record<string, string> {
  const out: Record<string, string> = {};
  let i = 0;
  while (i < body.length) {
    while (i < body.length && /\s/.test(body[i])) i++;
    const tagStart = i;
    while (i < body.length && body[i] !== '{') i++;
    const tag = body.slice(tagStart, i).trim();
    if (!tag || body[i] !== '{') break;
    const inner = extractPluralBody(body, i + 1);
    if (!inner) break;
    out[tag] = inner.body;
    i = inner.end + 1;
  }
  return out;
}

// ICU-aware placeholder substitution. Supports simple `{name}`, plural
// (ru rules), and select. Nested placeholders inside select/plural bodies
// resolve recursively. Number formatters are not covered.
function interpolate(template: string, params?: Record<string, unknown>): string {
  if (!params) return template;
  let out = '';
  let i = 0;
  while (i < template.length) {
    if (template[i] !== '{') {
      out += template[i++];
      continue;
    }
    const inner = extractPluralBody(template, i + 1);
    if (!inner) {
      out += template[i++];
      continue;
    }
    const expr = inner.body;
    const pluralIdx = expr.indexOf(', plural,');
    const selectIdx = expr.indexOf(', select,');
    if (pluralIdx !== -1) {
      const name = expr.slice(0, pluralIdx).trim();
      const value = params[name];
      const num = typeof value === 'number' ? value : Number(value);
      const opts = parsePluralOptions(expr.slice(pluralIdx + ', plural,'.length));
      const exact = opts[`=${num}`];
      const cat = ruPluralCategory(num);
      const chosen = exact ?? opts[cat] ?? opts.other ?? '';
      out += interpolate(chosen.replace(/#/g, String(num)), params);
    } else if (selectIdx !== -1) {
      const name = expr.slice(0, selectIdx).trim();
      const value = params[name];
      const opts = parsePluralOptions(expr.slice(selectIdx + ', select,'.length));
      const chosen = opts[String(value)] ?? opts.other ?? '';
      out += interpolate(chosen, params);
    } else {
      const name = expr.trim();
      const v = params[name];
      out += v === undefined || v === null ? `{${name}}` : String(v);
    }
    i = inner.end + 1;
  }
  return out;
}

vi.mock('next-intl', () => {
  const tFactory = (namespace?: string) => {
    const t = (key: string, params?: Record<string, unknown>) =>
      interpolate(lookupTranslation(namespace, key), params);
    (t as unknown as { has: (k: string) => boolean }).has = () => true;
    (t as unknown as { raw: (k: string) => unknown }).raw = (k: string) => lookupRaw(namespace, k);
    return t;
  };
  return {
    useTranslations: tFactory,
    createTranslator: ({ namespace }: { namespace?: string } = {}) => tFactory(namespace),
    useLocale: () => state.locale,
    useFormatter: () => ({
      dateTime: (d: Date) => d.toISOString(),
      number: (n: number) => String(n),
    }),
    NextIntlClientProvider: ({ children }: { children: React.ReactNode }) => children,
  };
});

// localStorage polyfill — jsdom's proxy doesn't expose prototype methods
// across the Vitest vm realm.
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => {
      store[key] = String(value);
    },
    removeItem: (key: string) => {
      delete store[key];
    },
    clear: () => {
      store = {};
    },
    get length() {
      return Object.keys(store).length;
    },
    key: (index: number) => Object.keys(store)[index] ?? null,
  };
})();

Object.defineProperty(globalThis, 'localStorage', {
  value: localStorageMock,
  writable: true,
});

// ResizeObserver polyfill — jsdom does not ship one, and Radix primitives
// (RadioGroup, Checkbox, etc.) read window.ResizeObserver at mount time.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof globalThis.ResizeObserver === 'undefined') {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).ResizeObserver = ResizeObserverStub;
}

// matchMedia stub defaults matches=false (mobile-first). Tests that need
// desktop chrome must call `setDesktopViewport()` from test-utils/viewport.
if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

// hasPointerCapture / releasePointerCapture / scrollIntoView — jsdom stubs
// used by Radix primitives. Only install if missing.
if (typeof (globalThis as unknown as { Element?: typeof Element }).Element !== 'undefined') {
  const proto = (globalThis as unknown as { Element: typeof Element }).Element
    .prototype as unknown as Record<string, unknown>;
  if (typeof proto.hasPointerCapture !== 'function') {
    proto.hasPointerCapture = function () {
      return false;
    };
  }
  if (typeof proto.releasePointerCapture !== 'function') {
    proto.releasePointerCapture = function () {};
  }
  if (typeof proto.scrollIntoView !== 'function') {
    proto.scrollIntoView = function () {};
  }
}

// axe a11y matchers. `@chialab/vitest-axe` is the React-18-compatible fork
// (`@axe-core/react` is NOT React-18-compatible).
//
// Must import the DEFAULT export from the main entry: the `./matchers`
// subpath in @chialab/vitest-axe@0.19.1 is types-only (no `default`
// runtime condition) and fails with "No known conditions" at runtime.
import axeMatchers from '@chialab/vitest-axe';
import { expect as vitestExpect } from 'vitest';
vitestExpect.extend(axeMatchers);
