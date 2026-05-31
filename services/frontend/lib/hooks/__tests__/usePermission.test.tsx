import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mock the API layer — usePermission goes through getMyPermissions.
vi.mock('@/lib/api/permissions', () => ({
  getMyPermissions: vi.fn(),
  getPermissionsCatalog: vi.fn(),
}));

// Mock the Zustand store so the test can flip activeBusinessId imperatively.
let storeActiveBusinessId: string | null = 'biz-1';
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: storeActiveBusinessId }),
}));

import { usePermission } from '@/lib/hooks/usePermission';
import { getMyPermissions } from '@/lib/api/permissions';

const mockedGetMyPermissions = vi.mocked(getMyPermissions);

function makeWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

beforeEach(() => {
  storeActiveBusinessId = 'biz-1';
  mockedGetMyPermissions.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('usePermission (reads /me/permissions)', () => {
  it('returns isLoading=true then allowed=true when API returns the perm', async () => {
    mockedGetMyPermissions.mockResolvedValue(['business.read', 'members.invite']);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => usePermission('members.invite'), {
      wrapper: makeWrapper(qc),
    });
    expect(result.current).toEqual({ allowed: false, isLoading: true });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.allowed).toBe(true);
  });

  it('returns allowed=false when the requested perm is absent from the API response', async () => {
    mockedGetMyPermissions.mockResolvedValue(['business.read']);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => usePermission('members.invite'), {
      wrapper: makeWrapper(qc),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.allowed).toBe(false);
  });

  it('does not fetch when activeBusinessId is null (enabled gate)', () => {
    storeActiveBusinessId = null;
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderHook(() => usePermission('business.read'), { wrapper: makeWrapper(qc) });
    expect(mockedGetMyPermissions).not.toHaveBeenCalled();
  });

  it('refetches when activeBusinessId changes (cache partition by business)', async () => {
    mockedGetMyPermissions.mockResolvedValue(['business.read']);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result, rerender } = renderHook(() => usePermission('business.read'), {
      wrapper: makeWrapper(qc),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(mockedGetMyPermissions).toHaveBeenCalledTimes(1);
    expect(mockedGetMyPermissions).toHaveBeenLastCalledWith('biz-1');

    act(() => {
      storeActiveBusinessId = 'biz-2';
    });
    rerender();
    await waitFor(() => expect(mockedGetMyPermissions).toHaveBeenCalledTimes(2));
    expect(mockedGetMyPermissions).toHaveBeenLastCalledWith('biz-2');
  });

  it('refetches when the query is invalidated (PermissionsCacheGuard contract)', async () => {
    // Asserts the second leg of the freshness contract: explicit cache
    // invalidation triggers a re-fetch. The first leg (passive 60 s polling
    // via refetchInterval) is a React-Query primitive — verified by reading
    // the option in source and by the hook's integration with the live API
    // in dev. Driving the interval deterministically here would require
    // mocking React-Query's internal tick scheduler, which couples the test
    // to library internals.
    mockedGetMyPermissions.mockResolvedValue(['business.read']);
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
    });
    const { result } = renderHook(() => usePermission('business.read'), {
      wrapper: makeWrapper(qc),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(mockedGetMyPermissions).toHaveBeenCalledTimes(1);

    await act(async () => {
      await qc.invalidateQueries({ queryKey: ['businesses', 'biz-1', 'permissions'] });
    });
    await waitFor(() => expect(mockedGetMyPermissions).toHaveBeenCalledTimes(2));
  });
});
