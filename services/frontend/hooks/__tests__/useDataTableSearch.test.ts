import { describe, expect, it, vi, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

import { useDataTableSearch } from '@/hooks/useDataTableSearch';

interface Row {
  id: string;
  content: string;
  author?: string;
}

const ROWS: Row[] = [
  { id: 'r1', content: 'Hello World', author: 'Alice' },
  { id: 'r2', content: 'Goodbye Mars', author: 'Bob' },
  { id: 'r3', content: 'Hello Mars', author: 'Carol' },
];

afterEach(() => {
  vi.useRealTimers();
});

describe('useDataTableSearch', () => {
  it('returns all rows when query is empty', () => {
    const { result } = renderHook(() =>
      useDataTableSearch<Row>({ rows: ROWS, searchableFields: (r) => [r.content] }),
    );
    expect(result.current.visibleRows).toEqual(ROWS);
  });

  it('filters rows by searchableFields output', () => {
    const { result } = renderHook(() =>
      useDataTableSearch<Row>({ rows: ROWS, searchableFields: (r) => [r.content] }),
    );

    act(() => {
      result.current.setQuery('mars');
    });

    expect(result.current.visibleRows).toHaveLength(2);
    expect(result.current.visibleRows.map((r) => r.id)).toEqual(['r2', 'r3']);
  });

  it('matches case-insensitively', () => {
    const { result } = renderHook(() =>
      useDataTableSearch<Row>({ rows: ROWS, searchableFields: (r) => [r.content] }),
    );

    act(() => {
      result.current.setQuery('HELLO');
    });

    expect(result.current.visibleRows.map((r) => r.id)).toEqual(['r1', 'r3']);
  });

  it('searches across multiple fields when caller returns multiple strings', () => {
    const { result } = renderHook(() =>
      useDataTableSearch<Row>({
        rows: ROWS,
        searchableFields: (r) => [r.content, r.author ?? ''],
      }),
    );

    // 'bob' only appears in author — not in content.
    act(() => {
      result.current.setQuery('bob');
    });
    expect(result.current.visibleRows).toHaveLength(1);
    expect(result.current.visibleRows[0].id).toBe('r2');
  });

  it('trims whitespace before matching', () => {
    const { result } = renderHook(() =>
      useDataTableSearch<Row>({ rows: ROWS, searchableFields: (r) => [r.content] }),
    );

    // Whitespace-only string matches as if empty.
    act(() => {
      result.current.setQuery('   ');
    });
    expect(result.current.visibleRows).toEqual(ROWS);
  });

  it('debounce delays the filter application', () => {
    vi.useFakeTimers();

    const { result } = renderHook(() =>
      useDataTableSearch<Row>({
        rows: ROWS,
        searchableFields: (r) => [r.content],
        debounceMs: 250,
      }),
    );

    act(() => {
      result.current.setQuery('mars');
    });

    // Before timer fires — visibleRows still reflects empty query (all rows).
    act(() => {
      vi.advanceTimersByTime(249);
    });
    expect(result.current.visibleRows).toHaveLength(3);

    // After 250 ms — debounced query lands.
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current.visibleRows.map((r) => r.id)).toEqual(['r2', 'r3']);
  });

  it('returns empty array when no row matches', () => {
    const { result } = renderHook(() =>
      useDataTableSearch<Row>({ rows: ROWS, searchableFields: (r) => [r.content] }),
    );

    act(() => {
      result.current.setQuery('zzznotfound');
    });
    expect(result.current.visibleRows).toEqual([]);
  });
});
