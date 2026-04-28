import { useEffect, useState } from 'react';

/**
 * Phase 19 / Plan 19-04 — generic debounce hook.
 *
 * Returns the latest `value` after `delayMs` of stability. Used by SidebarSearch
 * to gate the React Query fetch behind 250 ms of typing-stillness (SEARCH-04).
 *
 * Test with `vi.useFakeTimers()` + `vi.advanceTimersByTime(delayMs)` — the timer
 * is created with `setTimeout`, so fake timers fully control resolution.
 */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);
  return debounced;
}
