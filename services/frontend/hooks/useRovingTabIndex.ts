'use client';

import { useCallback, useEffect, useRef } from 'react';

/**
 * Phase 19 / Plan 19-05 / D-17 — roving-tabindex hook for chat-list portions
 * of the sidebar (ProjectSection, UnassignedBucket, PinnedSection).
 *
 * Behavior contract (locked at D-17, 19-CONTEXT.md):
 *   - Tab enters the list ONCE — focus lands on the first item.
 *   - ArrowDown / ArrowUp move focus between items (wrap-around).
 *   - Home / End jump to the first / last item.
 *   - Enter / Space falls through to the focused item's native handler
 *     (the consumer renders <Link> / <button> elements; this hook does
 *     not intercept Enter or Space).
 *   - Project-section headers, search input, and other controls are
 *     SEPARATE Tab stops — they live OUTSIDE the roving container.
 *
 * Each focusable item inside the container gets the attribute
 * `data-roving-item`. The hook reads them via `querySelectorAll` so the
 * consumer does NOT have to thread refs through every row.
 *
 * Pattern source: W3C ARIA Authoring Practices listbox keyboard model
 * (https://www.w3.org/WAI/ARIA/apg/patterns/listbox/) — there is no
 * in-repo precedent (PATTERNS §27 "NO IN-REPO ANALOG — flagged").
 *
 * @param itemCount — current number of `[data-roving-item]` elements
 *   inside the container. Used to bound arrow / Home / End navigation.
 *   The hook re-asserts initial tabindex={0,-1,...,-1} whenever itemCount
 *   changes (so list growth/shrink stays consistent).
 */
export function useRovingTabIndex(itemCount: number) {
  const containerRef = useRef<HTMLElement | null>(null);
  const focusedIdx = useRef(0);

  const focusItem = useCallback((idx: number) => {
    const items = containerRef.current?.querySelectorAll<HTMLElement>('[data-roving-item]');
    if (!items || items.length === 0) return;
    const clamped = Math.max(0, Math.min(items.length - 1, idx));
    items.forEach((el, i) => el.setAttribute('tabindex', i === clamped ? '0' : '-1'));
    items[clamped]?.focus();
    focusedIdx.current = clamped;
  }, []);

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLElement>) => {
      if (itemCount === 0) return;
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          focusItem((focusedIdx.current + 1) % itemCount);
          break;
        case 'ArrowUp':
          e.preventDefault();
          focusItem((focusedIdx.current - 1 + itemCount) % itemCount);
          break;
        case 'Home':
          e.preventDefault();
          focusItem(0);
          break;
        case 'End':
          e.preventDefault();
          focusItem(itemCount - 1);
          break;
      }
    },
    [itemCount, focusItem]
  );

  // Re-assert initial tabindex distribution whenever the item count
  // changes. The first roving item is the single Tab stop; all others
  // are tabindex=-1 (focusable only via the arrow keys above).
  useEffect(() => {
    const items = containerRef.current?.querySelectorAll<HTMLElement>('[data-roving-item]');
    items?.forEach((el, i) => el.setAttribute('tabindex', i === 0 ? '0' : '-1'));
    focusedIdx.current = 0;
  }, [itemCount]);

  return { containerRef, onKeyDown };
}
