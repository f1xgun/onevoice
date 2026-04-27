import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { ProjectPane } from '../ProjectPane';

// Mock next/navigation.
const pushMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock, back: vi.fn(), replace: vi.fn() }),
  usePathname: () => '/chat',
}));

// Mock sonner toast.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock axios-based API client.
vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: [] })),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

// Hooks: mock conversations + projects so ProjectPane sees a deterministic
// empty world by default.
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

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('ProjectPane', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    pushMock.mockReset();
  });

  it('renders the «Без проекта» bucket and the «+ Новый проект» link with empty data', () => {
    render(
      <Wrapper>
        <ProjectPane />
      </Wrapper>
    );
    expect(screen.getByText('Без проекта')).toBeInTheDocument();
    expect(screen.getByText('+ Новый проект')).toBeInTheDocument();
  });

  it('exposes placeholder slots for sidebar-search (19-04) and pinned-section (19-02)', () => {
    const { container } = render(
      <Wrapper>
        <ProjectPane />
      </Wrapper>
    );
    expect(container.querySelector('[data-testid="sidebar-search-slot"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="pinned-section-slot"]')).not.toBeNull();
  });

  it('exposes a data-testid="project-pane" wrapper for layout.tsx route gating', () => {
    const { container } = render(
      <Wrapper>
        <ProjectPane />
      </Wrapper>
    );
    expect(container.querySelector('[data-testid="project-pane"]')).not.toBeNull();
  });
});
