import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { api } from '@/lib/api';
import { queryClient } from '@/lib/queryClient';

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

const destinations = [
  ['Ждет вашего слова', '/inbox'],
  ['Поручения', '/chat'],
  ['Посты', '/posts'],
  ['Отзывы', '/reviews'],
  ['Карточка точки', '/business'],
  ['Площадки', '/integrations'],
  ['Настройки', '/settings'],
];

describe('NavRail', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    usePathnameMock.mockReturnValue('/chat');
  });

  it('renders the seven approved destinations without icons or project and task labels', () => {
    render(<NavRail />, { wrapper: Wrapper });
    for (const [label, href] of destinations) {
      const link = screen.getByRole('link', { name: label, exact: true });
      expect(link).toHaveAttribute('href', href);
      expect(link.querySelector('svg')).toBeNull();
    }
    expect(screen.getByTestId('nav-rail')).toHaveClass('w-[232px]');
    expect(screen.queryByRole('link', { name: 'Задачи' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'С чего начать' })).not.toBeInTheDocument();
    expect(screen.queryByTestId('integration-status')).not.toBeInTheDocument();
    expect(screen.queryByTestId('project-pane')).not.toBeInTheDocument();
  });

  it.each([
    ['/chat/conversation-id', 'Поручения'],
    ['/integrations', 'Площадки'],
    ['/settings/team', 'Команда'],
    ['/settings', 'Настройки'],
    ['/settings/roles', 'Команда'],
    ['/settings/roles/new', 'Команда'],
    ['/settings/account', 'Аккаунт'],
    ['/settings/audit', 'Данные и журнал'],
  ])('marks one active destination on %s', (pathname, label) => {
    usePathnameMock.mockReturnValue(pathname);
    const { container } = render(<NavRail />, { wrapper: Wrapper });
    expect(container.querySelectorAll('[aria-current="page"]')).toHaveLength(1);
    expect(screen.getByRole('link', { name: label, exact: true })).toHaveAttribute(
      'aria-current',
      'page'
    );
  });

  it('nests settings destinations and preserves the billing permission gate', () => {
    usePathnameMock.mockReturnValue('/settings/team');
    render(<NavRail />, { wrapper: Wrapper });
    for (const label of ['Правила', 'Команда', 'Аккаунт', 'Данные и журнал']) {
      expect(screen.getByRole('link', { name: label, exact: true })).toBeInTheDocument();
    }
    expect(screen.queryByRole('link', { name: 'Тариф' })).not.toBeInTheDocument();
  });

  it('keeps appearance, language, feedback and server logout reachable under the user', async () => {
    const clearSpy = vi.spyOn(queryClient, 'clear');
    render(<NavRail />, { wrapper: Wrapper });
    const user = userEvent.setup();
    await user.click(screen.getByText('X', { exact: true }));
    expect(screen.getByRole('button', { name: 'Language' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Обратная связь' })).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Выйти' }));
    expect(api.post).toHaveBeenCalledWith('/auth/logout');
    expect(clearSpy).toHaveBeenCalled();
    expect(logoutMock).toHaveBeenCalled();
    expect(pushMock).toHaveBeenCalledWith('/login');
  });

  it('closes the mobile menu when a destination is activated by keyboard', async () => {
    const onNavigate = vi.fn();
    render(<NavRail expanded onNavigate={onNavigate} />, { wrapper: Wrapper });
    const link = screen.getByRole('link', { name: 'Площадки' });
    link.focus();
    await userEvent.keyboard('{Enter}');
    expect(onNavigate).toHaveBeenCalledOnce();
  });
});

it.each([
  ['ru', 'Аккаунт'],
  ['en', 'Account'],
] as const)(
  'keeps account settings available in %s without organization permissions',
  (locale, label) => {
    (globalThis as unknown as { __setTestLocale: (locale: string) => void }).__setTestLocale(
      locale
    );
    usePathnameMock.mockReturnValue('/settings/account');
    render(<NavRail />, { wrapper: Wrapper });
    expect(screen.getByRole('link', { name: label, exact: true })).toHaveAttribute(
      'href',
      '/settings/account'
    );
  }
);
