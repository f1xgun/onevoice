import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import AppLayout from '@/app/(app)/layout';

// Pathname mock — toggled per test.
const usePathnameMock = vi.fn(() => '/chat');
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
  usePathname: () => usePathnameMock(),
}));

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
    { getState: () => ({ ...tokenStore, ...setters }) }
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

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/hooks/useConversations', async () => {
  const actual =
    await vi.importActual<typeof import('@/hooks/useConversations')>('@/hooks/useConversations');
  return {
    ...actual,
    useConversationsQuery: () => ({ data: [], isLoading: false, error: null }),
  };
});
vi.mock('@/hooks/useProjects', async () => {
  const actual = await vi.importActual<typeof import('@/hooks/useProjects')>('@/hooks/useProjects');
  return {
    ...actual,
    useProjectsQuery: () => ({ data: [], isLoading: false, error: null }),
  };
});

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('AppLayout route-conditional ProjectPane (D-14)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders both NavRail and ProjectPane on /chat', async () => {
    usePathnameMock.mockReturnValue('/chat');
    const { container } = render(
      <Wrapper>
        <AppLayout>
          <div>child</div>
        </AppLayout>
      </Wrapper>
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(container.querySelector('[data-testid="nav-rail"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="project-pane"]')).not.toBeNull();
  });

  it('renders ONLY NavRail on /integrations (project-pane hidden)', async () => {
    usePathnameMock.mockReturnValue('/integrations');
    const { container } = render(
      <Wrapper>
        <AppLayout>
          <div>child</div>
        </AppLayout>
      </Wrapper>
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(container.querySelector('[data-testid="nav-rail"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="project-pane"]')).toBeNull();
  });
});
