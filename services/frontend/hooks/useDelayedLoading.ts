'use client';

import { useEffect, useState } from 'react';

export const LOADING_REVEAL_DELAY_MS = 200;

/** Defers only the loading visual, never data, errors or action disabling. */
export function useDelayedLoading(pending: boolean, delayMs = LOADING_REVEAL_DELAY_MS) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    setVisible(false);
    if (!pending) return;
    const timer = window.setTimeout(() => setVisible(true), delayMs);
    return () => window.clearTimeout(timer);
  }, [pending, delayMs]);

  return pending && visible;
}
