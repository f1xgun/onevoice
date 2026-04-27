'use client';

import { useEffect } from 'react';
import { useSearchParams, usePathname, useRouter } from 'next/navigation';

const HIGHLIGHT_FLASH_MS = 1750; // CONTEXT.md D-08: 1.5–2 s range; 1750 ms midpoint.
const HIGHLIGHT_DATA_ATTR = 'data-highlight';

/**
 * Phase 19 / Plan 19-04 / D-08 / SEARCH-04 — when /chat/{id}?highlight={msgId}
 * is loaded (typically navigated to from the search dropdown), find the matched
 * message in the DOM, scroll it into center view, apply a flash class for
 * 1.75 s, then remove the class AND strip the query param so a manual refresh
 * doesn't re-fire.
 *
 * Depends on `MessageBubble` rendering each message with a `data-message-id`
 * attribute. Re-runs when `messagesReady` flips so the effect waits for
 * messages to actually mount before scrolling (SSE-loaded messages arrive
 * after mount).
 *
 * Selector uses `CSS.escape(target)` to guard against arbitrary characters in
 * a Mongo ObjectID hex / UUID (T-19-04-01 mitigation).
 */
export function useHighlightMessage(messagesReady: boolean) {
  const params = useSearchParams();
  const pathname = usePathname();
  const router = useRouter();

  useEffect(() => {
    if (!messagesReady) return;
    const target = params.get('highlight');
    if (!target) return;

    const el = document.querySelector<HTMLElement>(
      `[data-message-id="${CSS.escape(target)}"]`
    );
    if (!el) return;

    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    el.setAttribute(HIGHLIGHT_DATA_ATTR, 'true');

    const timeout = window.setTimeout(() => {
      el.removeAttribute(HIGHLIGHT_DATA_ATTR);
      router.replace(pathname, { scroll: false });
    }, HIGHLIGHT_FLASH_MS);

    return () => {
      window.clearTimeout(timeout);
      el.removeAttribute(HIGHLIGHT_DATA_ATTR);
    };
  }, [messagesReady, params, pathname, router]);
}
