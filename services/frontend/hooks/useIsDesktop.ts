'use client';

import { useEffect, useState } from 'react';

// `useIsDesktop` — viewport gate, true once the browser confirms the viewport
// is at the Tailwind `md` breakpoint (≥768 px) or wider.
//
// The hook deliberately returns `false` on the very first render (which also
// matches SSR / first hydration). That avoids hydration mismatch warnings:
// the server has no viewport, so it has to commit to one branch, and the
// client must match that commit on first paint. Returning `false` is the
// safer default — mobile chrome is always-present in DOM via the Sheet
// trigger but renders only as a 56 px top bar until the drawer opens, so
// briefly showing it on a desktop user is cheap. The desktop NavRail +
// PanelGroup only mount after this effect promotes to `true`.
//
// Concretely: layout shells use this to render EXACTLY ONE `<main>` and ONE
// `<h1>` (via the page's `<PageHeader>` child). Without that gate, both the
// mobile branch (`md:hidden`) and the desktop branch (`hidden md:flex`)
// stay in the DOM, axe-core sees both, and `landmark-one-main` +
// `heading-one` regress.
//
// Breakpoint matches Tailwind's `md` (768 px) — see tailwind.config.ts
// `screens.md`. Update both together if the breakpoint ever moves.
export function useIsDesktop(): boolean {
  const [isDesktop, setIsDesktop] = useState(false);

  useEffect(() => {
    // SSR-safe: `window` is undefined during prerender. Bail out so the
    // initial false stays put — the layout will render mobile chrome
    // pre-hydration, then promote on mount.
    //
    // jsdom (Vitest) DOES define `window` but historically did NOT ship
    // `window.matchMedia`. Recent versions ship a stub that returns a
    // MediaQueryList — but defensive bailout costs nothing and keeps the
    // hook usable under older jsdom without a setup-file polyfill.
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;

    const query = window.matchMedia('(min-width: 768px)');
    setIsDesktop(query.matches);

    // Safari < 14 only ships the deprecated `addListener` / `removeListener`
    // pair; modern browsers expose `addEventListener` on MediaQueryList.
    // Branch on availability so we keep working across both shapes.
    const handler = (e: MediaQueryListEvent) => setIsDesktop(e.matches);
    if (typeof query.addEventListener === 'function') {
      query.addEventListener('change', handler);
      return () => query.removeEventListener('change', handler);
    }
    query.addListener(handler);
    return () => query.removeListener(handler);
  }, []);

  return isDesktop;
}
