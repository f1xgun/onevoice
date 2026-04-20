import '@testing-library/jest-dom';

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
