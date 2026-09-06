/**
 * Viewport helpers for tests. The global `vitest.setup.ts` polyfill
 * defaults `window.matchMedia` to `matches: false` (mobile-first), matching
 * the app shell's initial state. Tests that need to assert against the
 * desktop chrome (nav-rail, project-pane, resize handle, etc.) must call
 * `setDesktopViewport()` so `useIsDesktop()` returns `true`.
 */

/**
 * Override `window.matchMedia` so any query that asks about `min-width: 1280px`
 * (the breakpoint `useIsDesktop` uses) reports `matches: true`. All other
 * queries continue to report `false`, matching mobile-first defaults.
 *
 * Call from `beforeEach` or inline at the top of a desktop-only test.
 */
export function setDesktopViewport(): void {
  window.matchMedia = (query: string) =>
    ({
      matches: query.includes('min-width: 1280px'),
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}
