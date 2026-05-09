import '@testing-library/jest-dom';
import { vi } from 'vitest';

// Global next-intl stub. Real translation plumbing lives at
// lib/i18n/request.ts and only matters in production / e2e — tests
// shouldn't have to mount NextIntlClientProvider just to render any
// component that calls useTranslations.
//
// The stub looks the key up in messages/ru.json so tests that assert
// Russian copy ('Чат', 'Профиль бизнеса', etc.) continue to find what
// they expect. Missing keys fall back to the namespaced key string so
// the failure surface is "saw 'nav.foo' instead of 'Чат'" rather than
// a context-not-found exception.
import ruMessages from './messages/ru.json';

function lookupRaw(namespace: string | undefined, key: string): unknown {
  const path = namespace ? `${namespace}.${key}`.split('.') : key.split('.');
  let cursor: unknown = ruMessages;
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

// Russian plural category for an integer per CLDR rules. Used by the
// ICU-plural branch of `interpolate` below so a key like
// `Минимум {count, plural, one {# символ} few {# символа} many {# символов} other {# символов}}`
// renders the right form ('Минимум 2 символа', 'Минимум 6 символов'). The
// real next-intl runtime does this through Intl.PluralRules; the mock
// hand-rolls the rule because jsdom's Intl is sufficient but pulling in
// the runtime here would mean importing all of next-intl just for tests.
function ruPluralCategory(n: number): 'one' | 'few' | 'many' | 'other' {
  if (!Number.isInteger(n)) return 'other';
  const mod10 = Math.abs(n) % 10;
  const mod100 = Math.abs(n) % 100;
  if (mod10 === 1 && mod100 !== 11) return 'one';
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return 'few';
  return 'many';
}

// Extract the body of `{<name>, plural, …}` starting at the `, plural,`
// keyword. Returns `[innerBody, endIndex]` where `innerBody` is the
// concatenation of every option block (e.g. `one {# x} few {# y} other {# z}`)
// and `endIndex` is the offset of the matching closing `}`. Tracks brace
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

// Parse the option list inside an ICU plural body — `one {…} few {…} =0 {…}`.
// Returns a map keyed by category / `=N` literal. Handles nested braces
// inside option bodies.
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

// ICU-aware placeholder substitution for tests. Supports three shapes:
//   - simple `{name}` → params[name]
//   - `{count, plural, =N {…} one {…} few {…} many {…} other {…}}` (ru rules)
//   - `{var, select, key {…} other {…}}` — chosen body is recursively
//     interpolated so nested `{otherVar}` placeholders inside the select
//     body still resolve. Number formatters are not covered.
function interpolate(template: string, params?: Record<string, unknown>): string {
  if (!params) return template;
  let out = '';
  let i = 0;
  while (i < template.length) {
    if (template[i] !== '{') {
      out += template[i++];
      continue;
    }
    // Find matching closing brace, tracking depth so nested option bodies
    // (`one {# X}`) don't terminate the outer placeholder early.
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
      // `#` placeholder = the count itself; remaining `{name}` slots are
      // resolved recursively (real ICU also drops back into normal scope).
      out += interpolate(chosen.replace(/#/g, String(num)), params);
    } else if (selectIdx !== -1) {
      const name = expr.slice(0, selectIdx).trim();
      const value = params[name];
      const opts = parsePluralOptions(expr.slice(selectIdx + ', select,'.length));
      const chosen = opts[String(value)] ?? opts.other ?? '';
      out += interpolate(chosen, params);
    } else {
      // Simple `{name}` placeholder.
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
    // Real next-intl returns the raw JSON node here (object, array, or
    // string). The previous stub coerced everything to a string, which
    // hid `t.raw('arrayKey')` callers (lib/quick-actions.ts).
    (t as unknown as { raw: (k: string) => unknown }).raw = (k: string) => lookupRaw(namespace, k);
    return t;
  };
  return {
    useTranslations: tFactory,
    // Module-level translator (lib/i18n/translator.ts → lib/schemas.ts,
    // lib/resolveErrorMap.ts). Real next-intl returns a strictly-typed
    // function; the test stub mirrors `useTranslations` so callers get
    // the same key/params behavior.
    createTranslator: ({ namespace }: { namespace?: string } = {}) => tFactory(namespace),
    useLocale: () => 'ru',
    useFormatter: () => ({
      dateTime: (d: Date) => d.toISOString(),
      number: (n: number) => String(n),
    }),
    NextIntlClientProvider: ({ children }: { children: React.ReactNode }) => children,
  };
});

// localStorage polyfill for jsdom in Vitest vm context
// jsdom's localStorage proxy does not expose prototype methods across vm realms
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

// Phase 19 / Plan 19-05 — axe a11y matchers (toHaveNoViolations etc.).
// `@chialab/vitest-axe` is the React-18-compatible fork (RESEARCH §3) —
// `@axe-core/react` is incompatible with React 18 and CANNOT be used here.
// Matcher API: `expect(await axe(container)).toHaveNoViolations()`.
//
// IMPORTANT: the package exposes the matchers object as the DEFAULT export
// of the main entry (`lib/index.js: export default { toHaveNoViolations }`).
// The `./matchers` subpath in @chialab/vitest-axe@0.19.1's `package.json`
// `exports` map is a TYPES-ONLY entry (no `default` runtime condition) —
// importing it at runtime fails with "No known conditions". We therefore
// import the default from the main entry and pass it to `expect.extend`.
// Type augmentation for `toHaveNoViolations` is not strictly required because
// our axe tests filter violations manually (impact-aware gate, see
// components/sidebar/__a11y__/sidebar-axe.test.tsx).
import axeMatchers from '@chialab/vitest-axe';
import { expect as vitestExpect } from 'vitest';
vitestExpect.extend(axeMatchers);
