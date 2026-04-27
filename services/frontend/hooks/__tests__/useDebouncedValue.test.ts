import { describe, expect, it, vi, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { act } from 'react';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';

describe('useDebouncedValue — Phase 19 / SEARCH-04', () => {
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
    // 249 ms after the change, value still old.
    act(() => {
      vi.advanceTimersByTime(249);
    });
    expect(result.current).toBe('a');

    // One more ms — fires.
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
    // Still no flip — each rerender restarted the timer.
    expect(result.current).toBe('a');

    // After 250 ms of stillness, only the LATEST value lands.
    act(() => {
      vi.advanceTimersByTime(250);
    });
    expect(result.current).toBe('d');
  });
});
