// hooks/useDataTableSearch.ts — composition primitive.
//
// Owns the (optionally debounced) text-search slice for list pages.
// The caller supplies `searchableFields(row)` returning an array of
// strings to match against — the pilot adoption (posts) returns
// `[p.content]`; reviews can later return `[r.text, r.authorName]`.
//
// Reuses the existing `useDebouncedValue` hook so we don't double-implement
// debouncing. Default debounce is 0 ms (matching the current synchronous
// behaviour of posts/page.tsx), but callers can opt in to delay for
// expensive search expressions.
'use client';
import { useMemo, useState } from 'react';

import { useDebouncedValue } from './useDebouncedValue';

export interface UseDataTableSearchOptions<T> {
  rows: T[];
  searchableFields: (row: T) => string[];
  debounceMs?: number;
}

export interface UseDataTableSearchResult<T> {
  query: string;
  setQuery: (q: string) => void;
  visibleRows: T[];
}

export function useDataTableSearch<T>(
  opts: UseDataTableSearchOptions<T>
): UseDataTableSearchResult<T> {
  const [query, setQuery] = useState('');
  // Skip the useDebouncedValue indirection when debounceMs is unset / 0:
  // useDebouncedValue uses setTimeout even at 0ms, which makes the filter
  // application asynchronous even when the caller asked for synchronous
  // behavior. The pre-split posts/page.tsx (lines 88-92) is synchronous;
  // preserving that means consumers without debounceMs see updates inline.
  const debouncedQuery = useDebouncedValue(query, opts.debounceMs ?? 0);
  const effectiveQuery = opts.debounceMs && opts.debounceMs > 0 ? debouncedQuery : query;

  const { rows, searchableFields } = opts;
  const visibleRows = useMemo(() => {
    if (!effectiveQuery.trim()) return rows;
    const q = effectiveQuery.trim().toLowerCase();
    return rows.filter((r) => searchableFields(r).some((s) => s.toLowerCase().includes(q)));
  }, [rows, effectiveQuery, searchableFields]);

  return { query, setQuery, visibleRows };
}
