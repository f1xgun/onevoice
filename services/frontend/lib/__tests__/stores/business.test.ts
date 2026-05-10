import { describe, it, expect, beforeEach } from 'vitest';
import { useBusinessStore } from '../../stores/business';

describe('useBusinessStore', () => {
  beforeEach(() => {
    // Reset the store state between tests
    useBusinessStore.setState({ activeBusinessId: null });
    // Clear localStorage
    localStorage.clear();
  });

  it('has null as default activeBusinessId', () => {
    expect(useBusinessStore.getState().activeBusinessId).toBeNull();
  });

  it('setActive updates activeBusinessId', () => {
    useBusinessStore.getState().setActive('test-uuid-123');
    expect(useBusinessStore.getState().activeBusinessId).toBe('test-uuid-123');
  });

  it('clear resets activeBusinessId to null', () => {
    useBusinessStore.getState().setActive('test-uuid-123');
    useBusinessStore.getState().clear();
    expect(useBusinessStore.getState().activeBusinessId).toBeNull();
  });

  it('persists to localStorage under onevoice.business key', () => {
    useBusinessStore.getState().setActive('uuid-to-persist');
    const raw = localStorage.getItem('onevoice.business');
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw!);
    // Zustand persist wraps state in { state: {...}, version: 0 }
    expect(parsed.state.activeBusinessId).toBe('uuid-to-persist');
  });

  it('localStorage reflects clear() — activeBusinessId becomes null', () => {
    useBusinessStore.getState().setActive('uuid-to-persist');
    useBusinessStore.getState().clear();
    const raw = localStorage.getItem('onevoice.business');
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw!);
    expect(parsed.state.activeBusinessId).toBeNull();
  });
});
