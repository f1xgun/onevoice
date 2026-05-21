'use client';

import { useEffect, useState } from 'react';

export function useIsDesktop(): boolean {
  // Lazy initial state from matchMedia so desktop users don't see a 1-frame
  // mobile-shell flash on first client render. Hydration mismatch is not a
  // concern: the consuming layout returns `null` while `!ready`, so the SSR
  // markup is discarded before this hook's value ever paints.
  const [isDesktop, setIsDesktop] = useState<boolean>(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return false; // SSR: keep mobile default
    }
    return window.matchMedia('(min-width: 768px)').matches;
  });

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;

    const query = window.matchMedia('(min-width: 768px)');
    // Re-sync in case the viewport changed between initial render and mount
    // (e.g. browser zoom, orientation change during hydration).
    setIsDesktop(query.matches);

    const handler = (e: MediaQueryListEvent) => setIsDesktop(e.matches);
    // Safari < 14 only ships the deprecated addListener / removeListener pair.
    if (typeof query.addEventListener === 'function') {
      query.addEventListener('change', handler);
      return () => query.removeEventListener('change', handler);
    }
    query.addListener(handler);
    return () => query.removeListener(handler);
  }, []);

  return isDesktop;
}
