import { describe, expect, it, vi, beforeEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import type * as ConvHooks from '@/hooks/useConversations';
import type * as ProjHooks from '@/hooks/useProjects';

const replaceMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: replaceMock, refresh: vi.fn() }),
  usePathname: () => '/chat',
}));

// No access token in the store: the layout takes the mount-time refresh path.
vi.mock('@/lib/auth', () => {
  const tokenStore = { user: null as unknown, accessToken: null as string | null };
  const setters = {
    setAuth: (user: unknown, token: string) => {
      tokenStore.user = user;
      tokenStore.accessToken = token;
    },
    setAccessToken: (token: string) => {
      tokenStore.accessToken = token;
    },
    logout: vi.fn(() => {
      tokenStore.user = null;
      tokenStore.accessToken = null;
    }),
  };
  const useAuthStore = Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { ...tokenStore, ...setters };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ ...tokenStore, ...setters }) }
  );
  return { useAuthStore };
});

const refreshAccessTokenMock = vi.fn();
vi.mock('@/lib/api/authFetch', () => ({
  refreshAccessToken: () => refreshAccessTokenMock(),
  authFetch: vi.fn(),
}));

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: { id: 'u1' } })),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

vi.mock('@/lib/telemetry', () => ({ trackEvent: vi.fn() }));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector?: (s: unknown) => unknown) => {
    const state = { activeBusinessId: 'biz-1', setActive: vi.fn(), clear: vi.fn() };
    return selector ? selector(state) : state;
  },
}));

vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => ({
    data: [
      {
        id: 'biz-1',
        name: 'Test',
        role: { id: 'r1', name: 'owner' },
        status: 'active',
        joined_at: '2024-01-01',
      },
    ],
    isLoading: false,
  }),
  BUSINESS_LIST_QUERY_KEY: ['businesses'],
}));

vi.mock('@/lib/queryClient', () => ({
  queryClient: { invalidateQueries: vi.fn(), clear: vi.fn() },
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));

vi.mock('@/hooks/useConversations', async () => {
  const actual = await vi.importActual<typeof ConvHooks>('@/hooks/useConversations');
  return { ...actual, useConversationsQuery: () => ({ data: [], isLoading: false, error: null }) };
});
vi.mock('@/hooks/useProjects', async () => {
  const actual = await vi.importActual<typeof ProjHooks>('@/hooks/useProjects');
  return { ...actual, useProjectsQuery: () => ({ data: [], isLoading: false, error: null }) };
});

import AppLayout from '@/app/(app)/layout';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderLayout() {
  return render(
    <Wrapper>
      <AppLayout>
        <div>child</div>
      </AppLayout>
    </Wrapper>
  );
}

describe('AppLayout mount-time session refresh', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.getState().logout();
    refreshAccessTokenMock.mockReset();
    vi.mocked(api.get).mockReset();
    vi.mocked(api.get).mockResolvedValue({ data: { id: 'u1' } });
  });

  it.each([
    { name: '429 rate limited', error: { response: { status: 429 } } },
    { name: '500 backend blip', error: { response: { status: 500 } } },
    { name: 'network failure with no response', error: new Error('Network Error') },
  ])('does not log the user out on $name', async ({ error }) => {
    refreshAccessTokenMock.mockRejectedValue(error);

    renderLayout();

    await waitFor(() => {
      expect(
        screen.getByText(
          'Не удалось восстановить сессию. Проверьте соединение и попробуйте ещё раз.'
        )
      ).toBeInTheDocument();
    });
    expect(replaceMock).not.toHaveBeenCalled();
  });

  it('redirects to /login only when the refresh cookie itself is rejected (401)', async () => {
    refreshAccessTokenMock.mockRejectedValue({ response: { status: 401 } });

    renderLayout();

    await waitFor(() => {
      expect(replaceMock).toHaveBeenCalledWith('/login');
    });
    expect(screen.queryByText('Повторить попытку')).toBeNull();
  });

  it('retries the refresh from the error surface without a reload', async () => {
    const user = userEvent.setup();
    refreshAccessTokenMock
      .mockRejectedValueOnce({ response: { status: 429 } })
      .mockImplementationOnce(async () => {
        useAuthStore.getState().setAccessToken('fresh-token');
        return 'fresh-token';
      });

    renderLayout();

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Повторить попытку' })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: 'Повторить попытку' }));

    await waitFor(() => {
      expect(screen.getByText('child')).toBeInTheDocument();
      expect(refreshAccessTokenMock).toHaveBeenCalledTimes(2);
    });
    expect(replaceMock).not.toHaveBeenCalled();
  });

  it.each([401, 429, 500])(
    'keeps profile failure %s retryable after refresh succeeds',
    async (status) => {
      refreshAccessTokenMock.mockImplementation(async () => {
        useAuthStore.getState().setAccessToken('fresh-token');
        return 'fresh-token';
      });
      vi.mocked(api.get).mockRejectedValueOnce({ response: { status } });
      renderLayout();

      await userEvent.click(await screen.findByRole('button', { name: 'Повторить попытку' }));

      expect(await screen.findByText('child')).toBeInTheDocument();
      expect(refreshAccessTokenMock).toHaveBeenCalledTimes(1);
      expect(replaceMock).not.toHaveBeenCalled();
      expect(screen.queryByRole('button', { name: 'Повторить попытку' })).toBeNull();
    }
  );

  it('does not load the profile after unmount during a shared refresh', async () => {
    let resolveRefresh!: (token: string) => void;
    refreshAccessTokenMock.mockReturnValue(
      new Promise<string>((resolve) => {
        resolveRefresh = resolve;
      })
    );
    const view = renderLayout();
    view.unmount();
    await act(async () => resolveRefresh('fresh-token'));
    expect(api.get).not.toHaveBeenCalled();
    expect(replaceMock).not.toHaveBeenCalled();
  });
});
