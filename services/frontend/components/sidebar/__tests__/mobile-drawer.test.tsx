import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Sidebar } from '@/components/sidebar';
import type { Project } from '@/types/project';
import type { Conversation } from '@/lib/conversations';

// The shared mobile navigation closes after selecting a destination.

// ----- Mocks -----

const pushMock = vi.fn();
let pathnameValue = '/chat/c-1';
vi.mock('next/navigation', () => ({
  usePathname: () => pathnameValue,
  useRouter: () => ({ push: pushMock, back: vi.fn(), replace: vi.fn() }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

const sampleProject: Project = {
  id: 'p-1',
  businessId: 'b-1',
  name: 'Отзывы',
  description: '',
  systemPrompt: '',
  whitelistMode: 'inherit',
  allowedTools: [],
  quickActions: [],
  createdAt: '2026-04-18T00:00:00Z',
  updatedAt: '2026-04-18T00:00:00Z',
};

const sampleConv: Conversation = {
  id: 'c-1',
  userId: 'u-1',
  businessId: 'b-1',
  projectId: 'p-1',
  title: 'Первый чат',
  titleStatus: 'auto',
  createdAt: '2026-04-18T00:00:00Z',
  updatedAt: '2026-04-18T00:00:00Z',
};

// Mock useBusinessStore so hooks read a stable activeBusinessId.
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-1' }),
}));

// Mock bizApi — all business-scoped lib calls go through bizApi(bizId).verb(path).
const bizApiGet = vi.fn();
const bizApiPost = vi.fn();
const bizApiDelete = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: (path: string, config?: unknown) => bizApiGet(bizId, path, config),
    post: (path: string, data?: unknown) => bizApiPost(bizId, path, data),
    put: vi.fn(),
    delete: (path: string) => bizApiDelete(bizId, path),
  }),
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

// Mock plain api for non-business-scoped calls (auth, NavRail integrations).
const apiGet = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    get: (...args: unknown[]) => apiGet(...args),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

function setupApi() {
  bizApiGet.mockImplementation((_bizId: string, path: string) => {
    if (path === '/conversations' || path.startsWith('/conversations')) {
      return Promise.resolve({ data: [sampleConv] });
    }
    if (path === '/projects' || path.startsWith('/projects')) {
      return Promise.resolve({ data: [sampleProject] });
    }
    if (path === '/search') {
      return Promise.resolve({ data: [] });
    }
    return Promise.resolve({ data: null });
  });

  apiGet.mockImplementation((url: string) => {
    if (url === '/integrations') {
      return Promise.resolve({ data: [] });
    }
    return Promise.resolve({ data: null });
  });
}

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('mobile drawer', () => {
  beforeEach(() => {
    bizApiGet.mockReset();
    bizApiPost.mockReset();
    bizApiDelete.mockReset();
    apiGet.mockReset();
    pushMock.mockReset();
    setupApi();
    pathnameValue = '/chat/c-1';
  });

  it.each(['Поручения', 'Посты', 'Отзывы', 'Карточка точки', 'Площадки', 'Настройки'])(
    'auto-closes when %s is selected',
    async (name) => {
      const user = userEvent.setup();
      render(<Sidebar />, { wrapper: Wrapper });
      await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
      expect(screen.getByRole('dialog')).toBeInTheDocument();
      await user.click(screen.getByRole('link', { name, exact: true }));
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    }
  );

  it('offers the nested settings destinations and closes after selecting one', async () => {
    pathnameValue = '/settings/team';
    const user = userEvent.setup();
    render(<Sidebar />, { wrapper: Wrapper });
    await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
    for (const name of ['Правила', 'Команда', 'Тариф', 'Аккаунт', 'Данные и журнал']) {
      expect(screen.getByRole('link', { name, exact: true })).toBeInTheDocument();
    }
    await user.click(screen.getByRole('link', { name: 'Правила', exact: true }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
