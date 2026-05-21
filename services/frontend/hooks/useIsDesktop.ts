'use client';

import { useEffect, useState } from 'react';

export function useIsDesktop(): boolean {
  // Default false to avoid hydration mismatch — SSR has no viewport, so we
  // commit to mobile and promote on mount.
  const [isDesktop, setIsDesktop] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;

    const query = window.matchMedia('(min-width: 768px)');
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
