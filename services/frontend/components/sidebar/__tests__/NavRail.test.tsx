import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { NavRail } from '../NavRail';

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
    post: vi.fn(),
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
    // Each nav item exposes an aria-label so we can find it via role + name
    // even though the visible text is replaced by an icon-only column.
    const labels = [
      'Чат',
      'Интеграции',
      'Профиль бизнеса',
      'Отзывы',
      'Посты',
      'Задачи',
      'Настройки',
    ];
    for (const label of labels) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument();
    }
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

  it('renders within a w-14 (or w-16) narrow rail container (56–64 px per D-14)', () => {
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
    // Active state under the Linen design (mock-shell.jsx): no bg change,
    // just `text-ink` on the icon + `aria-current="page"` + a 2 px ochre
    // bar rendered as an absolutely-positioned span (`bg-ochre`) inside
    // the link. Asserting all three keeps the contract honest.
    expect(active).toHaveAttribute('aria-current', 'page');
    expect(active.className).toMatch(/\btext-ink\b/);
    expect(active.querySelector('span.bg-ochre')).not.toBeNull();
  });

  it('logout button calls useAuthStore.logout()', async () => {
    render(
      <Wrapper>
        <NavRail />
      </Wrapper>
    );
    const user = userEvent.setup();
    const logoutBtn = screen.getByRole('button', { name: 'Выйти' });
    await user.click(logoutBtn);
    expect(logoutMock).toHaveBeenCalled();
  });
});
