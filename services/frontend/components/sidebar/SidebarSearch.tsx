'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { usePathname } from 'next/navigation';
import * as Popover from '@radix-ui/react-popover';
import { Loader2, Search } from 'lucide-react';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';
import { api } from '@/lib/api';
import type { SearchResult } from '@/types/search';
import type { Business } from '@/types/business';
import { SearchResultRow } from './SearchResultRow';

// Phase 19 / Plan 19-04 — sidebar inline search.
//
// Contract anchors (locked):
//   D-11: Cmd/Ctrl-K consumer event name MUST equal 19-01 broadcaster.
const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';
//   SEARCH-04: 250 ms debounce (locked).
const DEBOUNCE_MS = 250;
//   D-13: min query length = 2 chars; below that, dropdown does NOT open and
//   no fetch fires.
const MIN_QUERY = 2;
//   SEARCH-03: result limit per request.
const RESULT_LIMIT = 20;

/**
 * UA-detected placeholder per CONTEXT.md / D-11 — Mac shows ⌘K, others Ctrl-K.
 * SSR fallback is the non-Mac variant (matches the sidebar rail's static label
 * convention).
 */
function detectPlaceholder(): string {
  if (typeof navigator === 'undefined') return 'Поиск... Ctrl-K';
  const platform = navigator.platform ?? '';
  const userAgent = navigator.userAgent ?? '';
  if (/Mac|iPhone|iPad|iPod/.test(platform)) return 'Поиск... ⌘K';
  if (/Mac OS X|iPhone|iPad/.test(userAgent)) return 'Поиск... ⌘K';
  return 'Поиск... Ctrl-K';
}

/** GET /business returns the caller's business — used as the React Query
 * stable key partition so unrelated tenants (during multi-tenant futures)
 * never share a cache entry. NEVER sent in the search request body — the
 * search handler resolves businessID server-side from the bearer's userID. */
async function fetchBusinessId(): Promise<string | null> {
  try {
    const { data } = await api.get<Business>('/business');
    return data?.id ?? null;
  } catch {
    return null;
  }
}

export function SidebarSearch() {
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [isOpen, setIsOpen] = useState(false);
  const [scopeAllBusiness, setScopeAllBusiness] = useState(false);
  const debounced = useDebouncedValue(query, DEBOUNCE_MS);
  const pathname = usePathname();

  // Route-aware default scope (D-10).
  // /chat/projects/{id} → default to project; show «По всему бизнесу» checkbox.
  // /chat root          → default to entire business; do NOT render checkbox.
  const projectIdFromRoute = useMemo(() => {
    if (!pathname) return null;
    const m = pathname.match(/^\/chat\/projects\/([^/]+)/);
    return m ? m[1] : null;
  }, [pathname]);
  const isProjectScoped = projectIdFromRoute != null && !scopeAllBusiness;
  const effectiveProjectId = isProjectScoped ? projectIdFromRoute : null;

  // Reset checkbox on route change (RESEARCH §15 Q3 — D-10 default re-asserts
  // when navigating between chat root and project pages).
  useEffect(() => {
    setScopeAllBusiness(false);
  }, [projectIdFromRoute]);

  // Cache-key partition only — never sent to /search (handler resolves server-side).
  const { data: businessId = null } = useQuery<string | null>({
    queryKey: ['business', 'id'],
    queryFn: fetchBusinessId,
    staleTime: 5 * 60 * 1000,
  });

  const enabled = debounced.trim().length >= MIN_QUERY;

  const { data: results = [], isFetching } = useQuery<SearchResult[]>({
    queryKey: ['search', businessId, effectiveProjectId, debounced],
    enabled,
    queryFn: () =>
      api
        .get<SearchResult[]>('/search', {
          params: {
            q: debounced,
            ...(effectiveProjectId ? { project_id: effectiveProjectId } : {}),
            limit: RESULT_LIMIT,
          },
        })
        .then((r) => r.data ?? []),
  });

  // Cmd-K consumer (RESEARCH §8). 19-01 dispatches the event from a global
  // keydown listener at app/(app)/layout.tsx; we attach in this component so
  // the focus + select happens on the actual <input>.
  useEffect(() => {
    const input = inputRef.current;
    if (!input) return;
    function onFocus() {
      input?.focus();
      input?.select();
      setIsOpen(true);
    }
    window.addEventListener(SIDEBAR_FOCUS_EVENT, onFocus);
    return () => window.removeEventListener(SIDEBAR_FOCUS_EVENT, onFocus);
  }, []);

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Escape') {
      // D-11: Esc clears + closes + blurs in single keystroke.
      e.preventDefault();
      setQuery('');
      setIsOpen(false);
      inputRef.current?.blur();
    }
    // ↑/↓/Enter delegated to the dropdown's roving-tabindex list (Plan 19-05).
  }

  const popoverOpen = isOpen && enabled;
  const listboxId = 'sidebar-search-listbox';

  return (
    <Popover.Root open={popoverOpen} onOpenChange={setIsOpen}>
      <Popover.Anchor asChild>
        <div className="relative">
          <Search
            className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-ink-soft"
            size={14}
            aria-hidden
          />
          <input
            ref={inputRef}
            type="text"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={popoverOpen}
            aria-controls={listboxId}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setIsOpen(true);
            }}
            onKeyDown={onKeyDown}
            placeholder={detectPlaceholder()}
            className="w-full rounded-md border border-line bg-paper-sunken py-1 pl-7 pr-7 text-sm text-ink placeholder:text-ink-faint focus:border-ochre focus:outline-none"
          />
          {isFetching && (
            <Loader2
              className="absolute right-2 top-1/2 -translate-y-1/2 animate-spin text-ink-soft"
              size={14}
              aria-hidden
            />
          )}
        </div>
      </Popover.Anchor>
      <Popover.Portal>
        <Popover.Content
          align="start"
          sideOffset={4}
          id={listboxId}
          // role="listbox" requires children with role="option" — applying
          // it unconditionally and rendering an empty-state text node trips
          // the axe `aria-required-children` (critical) rule. We apply
          // role="listbox" only when there are real result rows; the empty
          // state uses role="status" (a polite live region — also satisfies
          // aria-controls because the element identity is stable).
          role={results.length > 0 ? 'listbox' : 'status'}
          aria-live={results.length === 0 ? 'polite' : undefined}
          aria-label={results.length > 0 ? 'Результаты поиска' : 'Состояние поиска'}
          className="z-50 max-h-96 w-[var(--radix-popover-trigger-width)] overflow-y-auto rounded-md border border-line bg-paper-raised p-1 shadow-ov-2"
          // Keep focus in the search <input> so the user can keep typing.
          onOpenAutoFocus={(e) => e.preventDefault()}
        >
          {projectIdFromRoute && (
            <label className="flex items-center gap-2 px-2 py-1 text-xs text-ink-soft">
              <input
                type="checkbox"
                checked={scopeAllBusiness}
                onChange={(e) => setScopeAllBusiness(e.target.checked)}
              />
              По всему бизнесу
            </label>
          )}
          {results.length === 0 && !isFetching && (
            // Compact inline empty — visual retuned to match mock-states.jsx
            // "Поиск не нашёл совпадений" (lines 126–135): ink-mid lead +
            // ink-soft hint. Phrasing preserved verbatim
            // ("Ничего не найдено по «{query}»") because it's covered by
            // SidebarSearch test contract; we keep the literal as one text
            // node so RTL `getByText` regex matching keeps working.
            <div className="px-3 py-3">
              <div className="text-[13px] leading-relaxed text-ink-mid">
                {`Ничего не найдено по «${debounced}»`}
              </div>
              <div className="mt-1 text-[12px] text-ink-soft">
                Попробуйте короче или поменяйте период.
              </div>
            </div>
          )}
          {results.map((r) => (
            <SearchResultRow
              key={r.conversationId}
              result={r}
              query={debounced}
              onSelect={() => setIsOpen(false)}
            />
          ))}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
