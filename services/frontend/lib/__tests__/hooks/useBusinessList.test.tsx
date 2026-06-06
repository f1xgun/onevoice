import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useBusinessList, BUSINESS_LIST_QUERY_KEY } from '../../hooks/useBusinessList';

// Mock the api module
vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
    interceptors: {
      request: { use: vi.fn() },
      response: { use: vi.fn() },
    },
  },
}));

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

describe('useBusinessList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns query state {data, isLoading, error}', async () => {
    const { api } = await import('@/lib/api');
    (api.get as ReturnType<typeof vi.fn>).mockResolvedValue({
      data: [
        {
          id: 'biz-1',
          name: 'Test Business',
          role: { id: 'role-1', name: 'owner' },
          status: 'active',
          joined_at: '2024-01-01T00:00:00Z',
        },
      ],
    });

    const wrapper = createWrapper();
    const { result } = renderHook(() => useBusinessList(), { wrapper });

    expect(result.current.isLoading).toBe(true);

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.data).toHaveLength(1);
    expect(result.current.data?.[0].id).toBe('biz-1');
  });

  it('queryFn calls api.get("/businesses")', async () => {
    const { api } = await import('@/lib/api');
    (api.get as ReturnType<typeof vi.fn>).mockResolvedValue({ data: [] });

    const wrapper = createWrapper();
    renderHook(() => useBusinessList(), { wrapper });

    await waitFor(() => {
      expect(api.get).toHaveBeenCalledWith('/businesses');
    });
  });

  it('query key is ["businesses"] exactly', () => {
    expect(BUSINESS_LIST_QUERY_KEY).toEqual(['businesses']);
  });

  it('BUSINESS_LIST_QUERY_KEY is a constant array', () => {
    expect(BUSINESS_LIST_QUERY_KEY[0]).toBe('businesses');
    expect(BUSINESS_LIST_QUERY_KEY.length).toBe(1);
  });
});
