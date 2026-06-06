import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Sidebar } from '@/components/sidebar';
import type { Project } from '@/types/project';
import type { Conversation } from '@/lib/conversations';

// mobile drawer auto-close behavior.
//
// LOCKED CONTRACT:
//   - Drawer auto-closes on chat-row select.
//   - Drawer STAYS OPEN on project-header expand/collapse.
//   - Drawer STAYS OPEN on pin/rename/delete context-menu actions.

// ----- Mocks -----

const pushMock = vi.fn();
let pathnameValue = '/chat';
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
    pathnameValue = '/chat';
  });

  it('auto-closes when a chat row is clicked', async () => {
    const user = userEvent.setup();
    render(<Sidebar />, { wrapper: Wrapper });

    await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByText('Первый чат')).toBeInTheDocument();
    });

    await user.click(screen.getByRole('link', { name: /Первый чат/ }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });

  it('stays open when the project header expand/collapse button is clicked', async () => {
    const user = userEvent.setup();
    render(<Sidebar />, { wrapper: Wrapper });

    await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByText('Отзывы')).toBeInTheDocument();
    });

    const collapseBtn = screen.getByRole('button', { name: /Свернуть «Отзывы»/ });
    await user.click(collapseBtn);

    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('stays open when the per-row context menu is opened', async () => {
    const user = userEvent.setup();
    const { container } = render(<Sidebar />, { wrapper: Wrapper });

    await user.click(screen.getByRole('button', { name: 'Открыть боковое меню' }));
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByText('Первый чат')).toBeInTheDocument();
    });

    const menuTriggers = screen.getAllByRole('button', { name: /Меню чата/ });
    await user.click(menuTriggers[0]);

    const dialog = container.ownerDocument.querySelector('[role="dialog"]');
    expect(dialog).not.toBeNull();
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });
});
