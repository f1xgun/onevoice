import { describe, expect, it } from 'vitest';
import { renderHook, act } from '@testing-library/react';

import { useDataTableFilters } from '@/hooks/useDataTableFilters';

interface Filters extends Record<string, string> {
  status: string;
  platform: string;
}

const DEFAULT_FILTERS: Filters = { status: 'all', platform: 'all' };

describe('useDataTableFilters', () => {
  it('defaultValue is the initial state', () => {
    const { result } = renderHook(() =>
      useDataTableFilters<Filters>({ defaultValue: DEFAULT_FILTERS })
    );
    expect(result.current.filters).toEqual({ status: 'all', platform: 'all' });
  });

  it('setFilter updates one key without affecting others', () => {
    const { result } = renderHook(() =>
      useDataTableFilters<Filters>({ defaultValue: DEFAULT_FILTERS })
    );

    act(() => {
      result.current.setFilter('status', 'published');
    });

    expect(result.current.filters.status).toBe('published');
    expect(result.current.filters.platform).toBe('all'); // untouched
  });

  it('queryString skips entries with value === "all"', () => {
    const { result } = renderHook(() =>
      useDataTableFilters<Filters>({ defaultValue: DEFAULT_FILTERS })
    );

    // Both filters at 'all' — empty query string.
    expect(result.current.queryString()).toBe('');

    // Set status; platform still 'all' so it should be omitted.
    act(() => {
      result.current.setFilter('status', 'published');
    });
    expect(result.current.queryString()).toBe('status=published');
  });

  it('queryString skips entries with empty string', () => {
    const { result } = renderHook(() =>
      useDataTableFilters<Filters>({ defaultValue: { status: '', platform: '' } })
    );
    expect(result.current.queryString()).toBe('');

    act(() => {
      result.current.setFilter('status', 'error');
    });
    expect(result.current.queryString()).toBe('status=error');
  });

  it('queryString URL-encodes values', () => {
    const { result } = renderHook(() =>
      useDataTableFilters<Filters>({ defaultValue: { status: 'all', platform: 'all' } })
    );

    act(() => {
      // A value containing characters URLSearchParams must encode.
      result.current.setFilter('platform', 'foo bar&baz');
    });

    // URLSearchParams encodes space as '+' and '&' as '%26'.
    expect(result.current.queryString()).toBe('platform=foo+bar%26baz');
  });

  it('setFilter is stable across renders (useCallback)', () => {
    const { result, rerender } = renderHook(() =>
      useDataTableFilters<Filters>({ defaultValue: DEFAULT_FILTERS })
    );
    const firstSetFilter = result.current.setFilter;
    rerender();
    expect(result.current.setFilter).toBe(firstSetFilter);
  });

  it('queryString reflects multiple non-default filters', () => {
    const { result } = renderHook(() =>
      useDataTableFilters<Filters>({ defaultValue: DEFAULT_FILTERS })
    );

    act(() => {
      result.current.setFilter('status', 'scheduled');
    });
    act(() => {
      result.current.setFilter('platform', 'telegram');
    });

    const qs = result.current.queryString();
    // Order of URLSearchParams entries follows insertion. Check by parsing.
    const params = new URLSearchParams(qs);
    expect(params.get('status')).toBe('scheduled');
    expect(params.get('platform')).toBe('telegram');
  });
});
