import { render, screen, waitFor, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { expect, it, vi } from 'vitest';
import ChatListPage from '../page';
import { listConversations } from '@/lib/conversations';
import { bizApi } from '@/lib/api/business-api';

const h = vi.hoisted(() => ({
  activeBusinessId: 'test-org' as string | null,
  push: vi.fn(),
  trackClick: vi.fn(),
}));

vi.mock('next/navigation', () => ({ useRouter: () => ({ push: h.push }) }));
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: true }) }));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (select: (state: { activeBusinessId: string | null }) => unknown) =>
    select({ activeBusinessId: h.activeBusinessId }),
}));
vi.mock('@/lib/conversations', () => ({ listConversations: vi.fn() }));
vi.mock('@/lib/api/business-api', () => ({ bizApi: vi.fn() }));
vi.mock('@/hooks/useProjects', () => ({
  useProjectsQuery: () => ({ data: [], isPending: false, isError: false, refetch: vi.fn() }),
}));
vi.mock('@/lib/telemetry', () => ({ trackClick: h.trackClick }));

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ChatListPage />
    </QueryClientProvider>
  );
}

it('separates pending data, a failed list and a successful empty retry', async () => {
  h.activeBusinessId = 'test-org';
  vi.mocked(listConversations).mockReturnValueOnce(new Promise(() => {}));
  renderPage();
  expect(screen.getByRole('status', { hidden: true })).toHaveAttribute(
    'data-loading-placeholder',
    'pending'
  );
  expect(screen.queryByText('Нет диалогов')).not.toBeInTheDocument();
  cleanup();
  vi.mocked(listConversations)
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValueOnce([]);
  renderPage();
  expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось');
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole('button', { name: 'Повторить' }));
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
  expect(screen.getByRole('status')).not.toHaveAttribute('aria-busy', 'true');
});

it('does not navigate to an old business conversation after a late create success', async () => {
  h.activeBusinessId = 'biz-A';
  h.push.mockReset();
  h.trackClick.mockReset();
  vi.mocked(listConversations).mockResolvedValue([]);
  let resolveCreate!: (value: { data: { id: string } }) => void;
  const create = new Promise<{ data: { id: string } }>((resolve) => {
    resolveCreate = resolve;
  });
  vi.mocked(bizApi).mockReturnValue({
    post: vi.fn(() => create),
  } as unknown as ReturnType<typeof bizApi>);

  const rendered = renderPage();
  await userEvent.click(await screen.findByRole('button', { name: 'Новый чат' }));
  h.activeBusinessId = 'biz-B';
  rendered.rerender(
    <QueryClientProvider client={new QueryClient()}>
      <ChatListPage />
    </QueryClientProvider>
  );
  resolveCreate({ data: { id: 'conversation-A' } });

  await waitFor(() =>
    expect(h.trackClick).toHaveBeenCalledWith('create_conversation', undefined, 'biz-A')
  );
  expect(h.push).not.toHaveBeenCalled();
});
