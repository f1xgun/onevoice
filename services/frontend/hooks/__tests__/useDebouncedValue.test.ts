import { describe, expect, it, vi, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { act } from 'react';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';

describe('useDebouncedValue', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns the initial value synchronously', () => {
    const { result } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: 'a' },
    });
    expect(result.current).toBe('a');
  });

  it('debounces with 250 ms delay — keeps initial at 249 ms, flips at 250 ms', () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: 'a' },
    });
    expect(result.current).toBe('a');

    rerender({ v: 'b' });
    act(() => {
      vi.advanceTimersByTime(249);
    });
    expect(result.current).toBe('a');

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe('b');
  });

  it('collapses rapid changes within delayMs to a single update', () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: 'a' },
    });

    rerender({ v: 'b' });
    act(() => {
      vi.advanceTimersByTime(100);
    });
    rerender({ v: 'c' });
    act(() => {
      vi.advanceTimersByTime(100);
    });
    rerender({ v: 'd' });
    expect(result.current).toBe('a');

    act(() => {
      vi.advanceTimersByTime(250);
    });
    expect(result.current).toBe('d');
  });
});
