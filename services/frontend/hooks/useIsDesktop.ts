'use client';

import { useEffect, useState } from 'react';

/** Returns the desktop media-query state, defaulting to mobile when matchMedia is unavailable. */
export function useIsDesktop(): boolean {
  const [isDesktop, setIsDesktop] = useState<boolean>(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return false;
    }
    return window.matchMedia('(min-width: 1280px)').matches;
  });

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;

    const query = window.matchMedia('(min-width: 1280px)');
    setIsDesktop(query.matches);

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
