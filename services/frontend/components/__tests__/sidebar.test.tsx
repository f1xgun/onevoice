import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Sidebar } from '../sidebar';
import type { Project } from '@/types/project';

// Mock next/navigation — usePathname drives the projects-subtree visibility gate (GAP-03).
const pathnameRef: { current: string } = { current: '/chat' };
vi.mock('next/navigation', () => ({
  usePathname: () => pathnameRef.current,
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({ data: [] })),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

vi.mock('@/lib/auth', () => ({
  useAuthStore: (
    selector: (state: { user: { email: string } | null; logout: () => void }) => unknown
  ) => selector({ user: { email: 'tester@example.com' }, logout: vi.fn() }),
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

vi.mock('@/hooks/useProjects', () => ({
  useProjectsQuery: () => ({ data: [sampleProject] }),
}));

vi.mock('@/hooks/useConversations', () => ({
  useConversationsQuery: () => ({ data: [] }),
  useCreateConversation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useMoveConversation: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

function Providers({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

async function renderAndOpenDrawer(pathname: string) {
  pathnameRef.current = pathname;
  const result = render(
    <Providers>
      <Sidebar />
    </Providers>
  );
  // Phase 19 D-14: Sidebar is now mobile-only. The desktop layout is
  // owned by app/(app)/layout.tsx (NavRail + ProjectPane). To assert
  // projects-subtree visibility (GAP-03 contract preserved) we open the
  // mobile drawer first.
  const trigger = screen.getByRole('button', { name: 'Открыть боковое меню' });
  await userEvent.setup().click(trigger);
  return result;
}

describe('Sidebar — projects subtree visibility (GAP-03 — preserved on mobile drawer)', () => {
  beforeEach(() => {
    pathnameRef.current = '/chat';
  });

  it('renders projects subtree on /chat (drawer open)', async () => {
    await renderAndOpenDrawer('/chat');
    expect(screen.getByText('Без проекта')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /\+ Новый проект/ })).toBeInTheDocument();
  });

  it('renders projects subtree on /chat/:id (drawer open)', async () => {
    await renderAndOpenDrawer('/chat/69e486f230986d87c50887cc');
    expect(screen.getByText('Без проекта')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /\+ Новый проект/ })).toBeInTheDocument();
  });

  it('renders projects subtree on /projects/new (drawer open)', async () => {
    await renderAndOpenDrawer('/projects/new');
    expect(screen.getByText('Без проекта')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /\+ Новый проект/ })).toBeInTheDocument();
  });

  it('renders projects subtree on /projects/:id (drawer open)', async () => {
    await renderAndOpenDrawer('/projects/55f5dafe-2cc0-4540-9783-9b831b248ea0');
    expect(screen.getByText('Без проекта')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /\+ Новый проект/ })).toBeInTheDocument();
  });

  it('renders projects subtree on /projects/:id/chats (drawer open)', async () => {
    await renderAndOpenDrawer('/projects/55f5dafe-2cc0-4540-9783-9b831b248ea0/chats');
    expect(screen.getByText('Без проекта')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /\+ Новый проект/ })).toBeInTheDocument();
  });

  it('hides projects subtree on /integrations (drawer open — guard against over-widening the gate)', async () => {
    await renderAndOpenDrawer('/integrations');
    expect(screen.queryByText('Без проекта')).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /\+ Новый проект/ })).not.toBeInTheDocument();
  });
});
