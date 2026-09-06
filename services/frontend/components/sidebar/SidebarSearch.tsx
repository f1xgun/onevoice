'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { usePathname } from 'next/navigation';
import * as Popover from '@radix-ui/react-popover';
import { Loader2, Search } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { useBusinessStore } from '@/lib/stores/business';
import type { SearchResult } from '@/types/search';
import { SearchResultRow } from './SearchResultRow';

// Sidebar inline search.
//
// Contract anchors (locked):
//   Cmd/Ctrl-K consumer event name MUST equal the broadcaster.
const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';
//   250 ms debounce (locked).
const DEBOUNCE_MS = 250;
//   Min query length = 2 chars; below that, dropdown does NOT open and
//   no fetch fires.
const MIN_QUERY = 2;
//   Result limit per request.
const RESULT_LIMIT = 20;

/**
 * UA-detected OS variant — Mac shows ⌘K, others Ctrl-K. SSR fallback is
 * the non-Mac variant (matches the sidebar rail's static label
 * convention). The actual i18n string is resolved via the
 * `sidebar.search.placeholder` ICU select template ({os}) so a locale
 * switch retranslates without touching this helper.
 */
function detectPlaceholderOS(): 'mac' | 'other' {
  if (typeof navigator === 'undefined') return 'other';
  const platform = navigator.platform ?? '';
  const userAgent = navigator.userAgent ?? '';
  if (/Mac|iPhone|iPad|iPod/.test(platform)) return 'mac';
  if (/Mac OS X|iPhone|iPad/.test(userAgent)) return 'mac';
  return 'other';
}

export function SidebarSearch() {
  const tSide = useTranslations('sidebar');
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [isOpen, setIsOpen] = useState(false);
  const [scopeAllBusiness, setScopeAllBusiness] = useState(false);
  const debounced = useDebouncedValue(query, DEBOUNCE_MS);
  const pathname = usePathname();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

  const projectIdFromRoute = useMemo(() => {
    if (!pathname) return null;
    const m = pathname.match(/^\/chat\/projects\/([^/]+)/);
    return m ? m[1] : null;
  }, [pathname]);
  const isProjectScoped = projectIdFromRoute != null && !scopeAllBusiness;
  const effectiveProjectId = isProjectScoped ? projectIdFromRoute : null;

  useEffect(() => {
    setScopeAllBusiness(false);
  }, [projectIdFromRoute]);

  const enabled = debounced.trim().length >= MIN_QUERY && !!activeBusinessId;

  const { data: results = [], isFetching } = useQuery<SearchResult[]>({
    queryKey: ['businesses', activeBusinessId, 'search', effectiveProjectId, debounced],
    enabled,
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<SearchResult[]>(BIZ_API_PATHS.SEARCH.ROOT, {
          params: {
            q: debounced,
            ...(effectiveProjectId ? { project_id: effectiveProjectId } : {}),
            limit: RESULT_LIMIT,
          },
        })
        .then((r) => r.data ?? []),
  });

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
      e.preventDefault();
      setQuery('');
      setIsOpen(false);
      inputRef.current?.blur();
    }
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
            aria-label={tSide('search.inputAria')}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setIsOpen(true);
            }}
            onKeyDown={onKeyDown}
            placeholder={tSide('search.placeholder', { os: detectPlaceholderOS() })}
            className="w-full rounded-md border border-line bg-paper-sunken py-1 pl-7 pr-7 text-sm text-ink placeholder:text-ink-faint focus:border-brand focus:outline-none"
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
          role={results.length > 0 ? 'listbox' : 'status'}
          aria-live={results.length === 0 ? 'polite' : undefined}
          aria-label={results.length > 0 ? tSide('search.resultsAria') : tSide('search.stateAria')}
          className="z-50 max-h-96 w-[var(--radix-popover-trigger-width)] overflow-y-auto rounded-md border border-line bg-paper-raised p-1 shadow-ov-2"
          onOpenAutoFocus={(e) => e.preventDefault()}
        >
          {projectIdFromRoute && (
            <label className="flex items-center gap-2 px-2 py-1 text-xs text-ink-soft">
              <input
                type="checkbox"
                checked={scopeAllBusiness}
                onChange={(e) => setScopeAllBusiness(e.target.checked)}
              />
              {tSide('scopeAllBusiness')}
            </label>
          )}
          {results.length === 0 && !isFetching && (
            <div className="px-3 py-3">
              <div className="text-[13px] leading-relaxed text-ink-mid">
                {tSide('search.noResultsByQuery', { query: debounced })}
              </div>
              <div className="mt-1 text-[12px] text-ink-soft">{tSide('noResults')}</div>
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
