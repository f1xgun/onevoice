// hooks/useDataTableFilters.ts — composition primitive.
//
// Owns filter state for list pages (posts, reviews, future). Exposes:
// - `filters`        — the current filter values
// - `setFilter(k,v)` — partial-update one key
// - `queryString`  — URLSearchParams string suitable for use in
// TanStack Query keys + fetch URLs
//
// `queryString` skips entries whose value is the empty string OR the
// literal `'all'` — preserves the existing posts/page.tsx semantics
// (current at lines 80-84 of the pre-split file): `'all'` means "no filter",
// not a server-side enum value.
'use client';
import { useCallback, useState } from 'react';

export interface UseDataTableFiltersOptions<F extends Record<string, string>> {
  defaultValue: F;
}

export interface UseDataTableFiltersResult<F extends Record<string, string>> {
  filters: F;
  setFilter: <K extends keyof F>(key: K, value: F[K]) => void;
  queryString: () => string;
}

export function useDataTableFilters<F extends Record<string, string>>(
  opts: UseDataTableFiltersOptions<F>
): UseDataTableFiltersResult<F> {
  const [filters, setFilters] = useState<F>(opts.defaultValue);

  const setFilter = useCallback(<K extends keyof F>(key: K, value: F[K]) => {
    setFilters((prev) => ({ ...prev, [key]: value }));
  }, []);

  const queryString = useCallback(() => {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(filters)) {
      if (v !== '' && v !== 'all') {
        params.set(k, v as string);
      }
    }
    return params.toString();
  }, [filters]);

  return { filters, setFilter, queryString };
}
