import { describe, expect, it, vi, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';

import { useUnsavedChangesPrompt } from '@/lib/hooks/useUnsavedChangesPrompt';

// `useUnsavedChangesPrompt` is the dirty-form guard used by RoleEditorForm
// (Plan 05-07). It wires `window.beforeunload` to a handler that calls
// `preventDefault()` + sets `returnValue` so browsers show the native
// «Покинуть сайт?» prompt before the page unloads. Modern Chrome/Firefox
// ignore the custom message text and show a generic dialog — that's by
// design and noted in the hook's JSDoc.

afterEach(() => {
  vi.restoreAllMocks();
});

describe('useUnsavedChangesPrompt', () => {
  it('attaches beforeunload listener when isDirty=true', () => {
    const addSpy = vi.spyOn(window, 'addEventListener');
    renderHook(() => useUnsavedChangesPrompt(true, 'Unsaved'));
    expect(addSpy).toHaveBeenCalledWith('beforeunload', expect.any(Function));
  });

  it('does NOT attach listener when isDirty=false', () => {
    const addSpy = vi.spyOn(window, 'addEventListener');
    renderHook(() => useUnsavedChangesPrompt(false, 'Unsaved'));
    const beforeUnloadCalls = addSpy.mock.calls.filter((c) => c[0] === 'beforeunload');
    expect(beforeUnloadCalls).toHaveLength(0);
  });

  it('removes listener on unmount when previously attached', () => {
    const removeSpy = vi.spyOn(window, 'removeEventListener');
    const { unmount } = renderHook(() => useUnsavedChangesPrompt(true, 'Unsaved'));
    unmount();
    const beforeUnloadCalls = removeSpy.mock.calls.filter((c) => c[0] === 'beforeunload');
    expect(beforeUnloadCalls.length).toBeGreaterThanOrEqual(1);
  });

  it('switches between attached / detached as isDirty flips', () => {
    const addSpy = vi.spyOn(window, 'addEventListener');
    const removeSpy = vi.spyOn(window, 'removeEventListener');
    const { rerender } = renderHook(
      ({ dirty }: { dirty: boolean }) => useUnsavedChangesPrompt(dirty, 'Unsaved'),
      { initialProps: { dirty: false } }
    );
    expect(addSpy.mock.calls.filter((c) => c[0] === 'beforeunload')).toHaveLength(0);
    rerender({ dirty: true });
    expect(addSpy.mock.calls.filter((c) => c[0] === 'beforeunload')).toHaveLength(1);
    rerender({ dirty: false });
    expect(removeSpy.mock.calls.filter((c) => c[0] === 'beforeunload')).toHaveLength(1);
  });

  it('handler calls preventDefault and sets returnValue to the message', () => {
    renderHook(() => useUnsavedChangesPrompt(true, 'Unsaved!'));
    const event = new Event('beforeunload', { cancelable: true }) as BeforeUnloadEvent;
    Object.defineProperty(event, 'returnValue', { writable: true, value: '' });
    const preventSpy = vi.spyOn(event, 'preventDefault');
    window.dispatchEvent(event);
    expect(preventSpy).toHaveBeenCalled();
    expect(event.returnValue).toBe('Unsaved!');
  });
});
