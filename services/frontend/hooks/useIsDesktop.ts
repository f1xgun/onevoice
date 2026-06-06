'use client';

import { useEffect, useState } from 'react';

export function useIsDesktop(): boolean {
  const [isDesktop, setIsDesktop] = useState<boolean>(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return false; // SSR: keep mobile default
    }
    return window.matchMedia('(min-width: 768px)').matches;
  });

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;

    const query = window.matchMedia('(min-width: 768px)');
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
