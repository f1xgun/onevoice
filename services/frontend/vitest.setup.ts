import '@testing-library/jest-dom';
import { vi } from 'vitest';

// Global next-intl stub. Real translation plumbing lives at
// lib/i18n/request.ts and only matters in production / e2e — tests
// shouldn't have to mount NextIntlClientProvider just to render any
// component that calls useTranslations.
//
// The stub returns the (namespaced) key as the rendered string. Tests
// that assert specific user-visible Russian copy continue to work
// against the actual rendered messages they don't go through this mock
// (toasts surfacing server-supplied strings, locked Russian copy from
// API responses, etc.). For tests that *do* need the literal Russian
// from messages/ru.json, swap this stub for an explicit
// NextIntlClientProvider wrapper at the call site.
vi.mock('next-intl', () => {
  const tFactory = (namespace?: string) => {
    const t = (key: string) => (namespace ? `${namespace}.${key}` : key);
    // next-intl exposes raw / has on the t-function; provide cheap stubs
    // so tests that do {t.has('foo')} don't blow up.
    (t as unknown as { has: (k: string) => boolean }).has = () => true;
    (t as unknown as { raw: (k: string) => string }).raw = (k: string) =>
      namespace ? `${namespace}.${k}` : k;
    return t;
  };
  return {
    useTranslations: tFactory,
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
