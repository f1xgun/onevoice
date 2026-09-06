import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { NavRail } from '../NavRail';
import { api } from '@/lib/api';
import { queryClient } from '@/lib/queryClient';

// Mock next/navigation
const pushMock = vi.fn();
const usePathnameMock = vi.fn(() => '/chat');
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock, back: vi.fn(), replace: vi.fn() }),
  usePathname: () => usePathnameMock(),
}));

// Mock sonner toast.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock axios-based API client.
vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: [] })),
    post: vi.fn(() => Promise.resolve({ data: {} })),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

// Mock auth store with a logout spy. NavRail's logout button must trigger this.
const logoutMock = vi.fn();
vi.mock('@/lib/auth', () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = {
        logout: logoutMock,
        user: { id: 'u-1', email: 'x@y.z', name: 'X', role: 'owner' as const },
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        logout: logoutMock,
        user: { id: 'u-1', email: 'x@y.z', name: 'X', role: 'owner' as const },
      }),
    }
  ),
}));

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('NavRail', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    pushMock.mockReset();
    logoutMock.mockReset();
    usePathnameMock.mockReturnValue('/chat');
  });

  it('renders all 7 nav links from existing navItems set', () => {
    render(
      <Wrapper>
        <NavRail />
      </Wrapper>
    );
    const labels = [
      'Чат',
      'Интеграции',
      'Профиль организации',
      'Отзывы',
      'Посты',
      'Задачи',
      'Настройки',
    ];
    for (const label of labels) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument();
    }
  });

  it('renders the getting-started entry linking to /getting-started', () => {
    render(
      <Wrapper>
        <NavRail />
      </Wrapper>
    );
    const link = screen.getByRole('link', { name: 'С чего начать' });
    expect(link).toHaveAttribute('href', '/getting-started');
  });

  it('does NOT render the project-tree subtree (UnassignedBucket / ProjectSection / + Новый проект)', () => {
    usePathnameMock.mockReturnValue('/chat');
    render(
      <Wrapper>
        <NavRail />
      </Wrapper>
    );
    expect(screen.queryByText('Без проекта')).toBeNull();
    expect(screen.queryByText('+ Новый проект')).toBeNull();
  });

  it('renders within a w-14 (or w-16) narrow rail container (56–64 px)', () => {
    const { container } = render(
      <Wrapper>
        <NavRail />
      </Wrapper>
    );
    const rail = container.querySelector('[data-testid="nav-rail"]');
    expect(rail).not.toBeNull();
    expect(rail?.className ?? '').toMatch(/\bw-(14|16)\b/);
  });

  it('marks the active route with the Linen indicator (ink text + ochre left bar)', () => {
    usePathnameMock.mockReturnValue('/integrations');
    render(
      <Wrapper>
        <NavRail />
      </Wrapper>
    );
    const active = screen.getByRole('link', { name: 'Интеграции' });
    expect(active).toHaveAttribute('aria-current', 'page');
    expect(active.className).toMatch(/\btext-ink\b/);
    expect(active.querySelector('span.bg-ochre')).not.toBeNull();
  });

  it('logout button revokes the server session via POST /auth/logout then clears local state', async () => {
    const clearSpy = vi.spyOn(queryClient, 'clear');
    render(
      <Wrapper>
        <NavRail />
      </Wrapper>
    );
    const user = userEvent.setup();
    const logoutBtn = screen.getByRole('button', { name: 'Выйти' });
    await user.click(logoutBtn);
    expect(api.post).toHaveBeenCalledWith('/auth/logout');
    expect(clearSpy).toHaveBeenCalled();
    expect(logoutMock).toHaveBeenCalled();
  });
});

it('shows text labels and supports keyboard navigation in the mobile menu', async () => {
  const onNavigate = vi.fn();
  render(
    <Wrapper>
      <NavRail expanded onNavigate={onNavigate} />
    </Wrapper>
  );
  const link = screen.getByRole('link', { name: 'Интеграции' });
  expect(link).toHaveTextContent('Интеграции');
  expect(screen.getByTestId('nav-rail')).toHaveClass('w-full', 'h-auto');
  link.focus();
  await userEvent.keyboard('{Enter}');
  expect(onNavigate).toHaveBeenCalledOnce();
});
