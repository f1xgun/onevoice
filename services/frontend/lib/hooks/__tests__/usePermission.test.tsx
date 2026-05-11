import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: vi.fn(),
}));
vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: vi.fn(),
  BUSINESS_LIST_QUERY_KEY: ['businesses'],
}));

import { usePermission } from '@/lib/hooks/usePermission';
import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';

const mockedStore = vi.mocked(useBusinessStore);
const mockedList = vi.mocked(useBusinessList);

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  mockedStore.mockReset();
  mockedList.mockReset();
});

function arrange(roleName: string | null, isLoading = false) {
  mockedStore.mockImplementation((selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: roleName ? 'biz-1' : null })
  );
  mockedList.mockReturnValue({
    data: roleName
      ? [
          {
            id: 'biz-1',
            name: 'Acme',
            role: { id: 'r1', name: roleName },
            status: 'active',
            joined_at: '',
          },
        ]
      : [],
    isLoading,
  } as unknown as ReturnType<typeof useBusinessList>);
}

describe('usePermission', () => {
  it('returns { allowed: true } for admin members.invite', () => {
    arrange('admin');
    const { result } = renderHook(() => usePermission('members.invite'), { wrapper });
    expect(result.current).toEqual({ allowed: true, isLoading: false });
  });

  it('returns { allowed: false } for viewer members.invite', () => {
    arrange('viewer');
    const { result } = renderHook(() => usePermission('members.invite'), { wrapper });
    expect(result.current).toEqual({ allowed: false, isLoading: false });
  });

  it('returns { allowed: true } for owner asking any perm (sentinel)', () => {
    arrange('owner');
    const { result } = renderHook(() => usePermission('made.up.permission'), { wrapper });
    expect(result.current).toEqual({ allowed: true, isLoading: false });
  });

  it('returns { allowed: false, isLoading: true } while list is loading', () => {
    arrange('admin', true);
    const { result } = renderHook(() => usePermission('members.invite'), { wrapper });
    expect(result.current).toEqual({ allowed: false, isLoading: true });
  });

  it('returns { allowed: false } when no active business is set', () => {
    arrange(null);
    const { result } = renderHook(() => usePermission('members.read'), { wrapper });
    expect(result.current).toEqual({ allowed: false, isLoading: false });
  });
});
