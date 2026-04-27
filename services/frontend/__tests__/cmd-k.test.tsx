import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import AppLayout from '@/app/(app)/layout';

// Skip the auth bootstrap by pre-seeding a token before AppLayout mounts.
vi.mock('@/lib/auth', async () => {
  const tokenStore = { user: null as unknown, accessToken: 'test-token', isAuthenticated: true };
  const setters = {
    setAuth: (user: unknown, token: string) => {
      tokenStore.user = user;
      tokenStore.accessToken = token;
      tokenStore.isAuthenticated = true;
    },
    setAccessToken: (token: string) => {
      tokenStore.accessToken = token;
      tokenStore.isAuthenticated = !!token;
    },
    logout: () => {
      tokenStore.user = null;
      tokenStore.accessToken = null;
      tokenStore.isAuthenticated = false;
    },
  };
  const useAuthStore = Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { ...tokenStore, ...setters };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({ ...tokenStore, ...setters }),
    }
  );
  return { useAuthStore };
});

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: [] })),
    post: vi.fn(() => Promise.resolve({ data: { accessToken: 'test-token' } })),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

vi.mock('@/lib/telemetry', () => ({
  trackEvent: vi.fn(),
}));

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
  usePathname: () => '/chat',
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('Cmd/Ctrl-K global focus listener (D-11)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('dispatches a CustomEvent("onevoice:sidebar-search-focus") on Cmd+K', async () => {
    const spy = vi.fn();
    window.addEventListener('onevoice:sidebar-search-focus', spy);

    render(
      <Wrapper>
        <AppLayout>
          <div>child</div>
        </AppLayout>
      </Wrapper>
    );

    // Layout's auth-bootstrap is mount-only; flush microtasks until ready.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    await act(async () => {
      window.dispatchEvent(new KeyboardEvent('keydown', { metaKey: true, key: 'k' }));
    });

    expect(spy).toHaveBeenCalledTimes(1);
    window.removeEventListener('onevoice:sidebar-search-focus', spy);
  });

  it('also fires on Ctrl+K (non-Mac platforms)', async () => {
    const spy = vi.fn();
    window.addEventListener('onevoice:sidebar-search-focus', spy);

    render(
      <Wrapper>
        <AppLayout>
          <div>child</div>
        </AppLayout>
      </Wrapper>
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    await act(async () => {
      window.dispatchEvent(new KeyboardEvent('keydown', { ctrlKey: true, key: 'k' }));
    });

    expect(spy).toHaveBeenCalledTimes(1);
    window.removeEventListener('onevoice:sidebar-search-focus', spy);
  });
});
