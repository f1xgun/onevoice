import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mock next/navigation
const mockReplace = vi.fn();
const mockPush = vi.fn();
const usePathnameMock = vi.fn(() => '/chat');

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: mockReplace, push: mockPush }),
  usePathname: () => usePathnameMock(),
}));

// Mock useBusinessList hook
const useBusinessListMock = vi.fn();
vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => useBusinessListMock(),
  BUSINESS_LIST_QUERY_KEY: ['businesses'],
}));

// Mock useBusinessStore
let storeActiveBusinessId: string | null = null;
const setActiveMock = vi.fn((id: string | null) => {
  storeActiveBusinessId = id;
});
const clearMock = vi.fn(() => {
  storeActiveBusinessId = null;
});

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector?: (s: unknown) => unknown) => {
    const state = {
      activeBusinessId: storeActiveBusinessId,
      setActive: setActiveMock,
      clear: clearMock,
    };
    return selector ? selector(state) : state;
  },
}));

// Mock api and its dependencies to avoid side effects from api.ts
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

vi.mock('@/lib/queryClient', () => ({
  queryClient: { invalidateQueries: vi.fn() },
}));

vi.mock('@/lib/auth', () => ({
  useAuthStore: {
    getState: vi.fn(() => ({ accessToken: null })),
  },
}));

import { BusinessRequiredGuard } from '@/components/BusinessRequiredGuard';

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('BusinessRequiredGuard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    storeActiveBusinessId = null;
    mockReplace.mockClear();
    setActiveMock.mockClear();
    clearMock.mockClear();
  });

  it('Test 1: /login renders children directly (bypass)', async () => {
    usePathnameMock.mockReturnValue('/login');
    useBusinessListMock.mockReturnValue({ data: undefined, isLoading: true });

    const { getByText } = render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    expect(getByText('protected')).toBeTruthy();
  });

  it('Test 2: /register renders children directly (bypass)', async () => {
    usePathnameMock.mockReturnValue('/register');
    useBusinessListMock.mockReturnValue({ data: undefined, isLoading: true });

    const { getByText } = render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    expect(getByText('protected')).toBeTruthy();
  });

  it('Test 3: /onboarding renders children directly (bypass)', async () => {
    usePathnameMock.mockReturnValue('/onboarding');
    useBusinessListMock.mockReturnValue({ data: undefined, isLoading: true });

    const { getByText } = render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    expect(getByText('protected')).toBeTruthy();
  });

  it('Test 4: isLoading renders loading spinner (no children)', async () => {
    usePathnameMock.mockReturnValue('/chat');
    useBusinessListMock.mockReturnValue({ data: undefined, isLoading: true });

    const { queryByText, getByRole } = render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    // Children must not be rendered during loading.
    expect(queryByText('protected')).toBeNull();
    // A loading indicator must be visible instead of a blank screen.
    expect(getByRole('status')).toBeTruthy();
  });

  it('Test 5: businesses=[] redirects to /onboarding and sets activeBusinessId to null', async () => {
    usePathnameMock.mockReturnValue('/chat');
    useBusinessListMock.mockReturnValue({ data: [], isLoading: false });
    storeActiveBusinessId = 'old-id';

    render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(mockReplace).toHaveBeenCalledWith('/onboarding');
    expect(setActiveMock).toHaveBeenCalledWith(null);
  });

  it('Test 6: data.length >= 1, activeBusinessId is null -> setActive(data[0].id) called', async () => {
    usePathnameMock.mockReturnValue('/chat');
    storeActiveBusinessId = null;
    useBusinessListMock.mockReturnValue({
      data: [
        {
          id: 'biz-1',
          name: 'Business 1',
          role: { id: 'r1', name: 'owner' },
          status: 'active',
          joined_at: '2024-01-01',
        },
      ],
      isLoading: false,
    });

    render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(setActiveMock).toHaveBeenCalledWith('biz-1');
  });

  it('Test 7: data.length >= 1, activeBusinessId is in data -> renders children (no setActive)', async () => {
    usePathnameMock.mockReturnValue('/chat');
    storeActiveBusinessId = 'biz-1';
    useBusinessListMock.mockReturnValue({
      data: [
        {
          id: 'biz-1',
          name: 'Business 1',
          role: { id: 'r1', name: 'owner' },
          status: 'active',
          joined_at: '2024-01-01',
        },
      ],
      isLoading: false,
    });

    const { getByText } = render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(getByText('protected')).toBeTruthy();
    expect(setActiveMock).not.toHaveBeenCalled();
  });

  it('Test 8: activeBusinessId stale (not in data) -> setActive(data[0].id)', async () => {
    usePathnameMock.mockReturnValue('/chat');
    storeActiveBusinessId = 'stale-biz-id';
    useBusinessListMock.mockReturnValue({
      data: [
        {
          id: 'biz-1',
          name: 'Business 1',
          role: { id: 'r1', name: 'owner' },
          status: 'active',
          joined_at: '2024-01-01',
        },
        {
          id: 'biz-2',
          name: 'Business 2',
          role: { id: 'r2', name: 'admin' },
          status: 'active',
          joined_at: '2024-01-02',
        },
      ],
      isLoading: false,
    });

    render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(setActiveMock).toHaveBeenCalledWith('biz-1');
  });

  it('Test 9: error state renders retry UI with a retry button (not a spinner)', async () => {
    usePathnameMock.mockReturnValue('/chat');
    const refetchMock = vi.fn().mockResolvedValue(undefined);
    useBusinessListMock.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Failed to fetch'),
      refetch: refetchMock,
    });

    const { queryByRole, getByRole } = render(
      <Wrapper>
        <BusinessRequiredGuard>
          <div>protected</div>
        </BusinessRequiredGuard>
      </Wrapper>
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    // Must NOT show the loading spinner.
    expect(queryByRole('status')).toBeNull();

    // Must show a retry button.
    const retryButton = getByRole('button');
    expect(retryButton).toBeTruthy();

    // Clicking the retry button calls refetch.
    retryButton.click();
    expect(refetchMock).toHaveBeenCalledTimes(1);
  });
});
