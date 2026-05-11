import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

vi.mock('@/lib/api/permissions', () => ({
  getPermissionsCatalog: vi.fn(),
  getMyPermissions: vi.fn(),
}));

import { usePermissionsCatalog } from '@/lib/hooks/usePermissionsCatalog';
import { getPermissionsCatalog } from '@/lib/api/permissions';
import type { PermissionGroup } from '@/lib/schemas';

const mocked = vi.mocked(getPermissionsCatalog);

function makeWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

const fixture: PermissionGroup[] = [
  {
    resource: 'business',
    permissions: [{ name: 'business.read', description: 'Видеть профиль' }],
  },
];

beforeEach(() => {
  mocked.mockReset();
});

describe('usePermissionsCatalog', () => {
  it('fetches the catalog on first mount and returns parsed groups', async () => {
    mocked.mockResolvedValue(fixture);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => usePermissionsCatalog(), {
      wrapper: makeWrapper(qc),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(fixture);
    expect(mocked).toHaveBeenCalledTimes(1);
  });

  it('does NOT refetch on re-render within the same QueryClient (staleTime: Infinity)', async () => {
    mocked.mockResolvedValue(fixture);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = makeWrapper(qc);
    const first = renderHook(() => usePermissionsCatalog(), { wrapper });
    await waitFor(() => expect(first.result.current.isSuccess).toBe(true));
    expect(mocked).toHaveBeenCalledTimes(1);

    // Mount the hook a second time — staleTime: Infinity must keep the
    // cached entry so no second network call fires.
    const second = renderHook(() => usePermissionsCatalog(), { wrapper });
    await waitFor(() => expect(second.result.current.isSuccess).toBe(true));
    expect(mocked).toHaveBeenCalledTimes(1);
  });

  it('exposes the error state when the API rejects', async () => {
    mocked.mockRejectedValue(new Error('catalog 500'));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => usePermissionsCatalog(), {
      wrapper: makeWrapper(qc),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as Error).message).toContain('catalog 500');
  });
});
