import { render, screen, waitFor, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { expect, it, vi } from 'vitest';
import ChatListPage from '../page';
import { listConversations } from '@/lib/conversations';

vi.mock('next/navigation', () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: true }) }));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (select: (state: { activeBusinessId: string }) => unknown) =>
    select({ activeBusinessId: 'test-org' }),
}));
vi.mock('@/lib/conversations', () => ({ listConversations: vi.fn() }));

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ChatListPage />
    </QueryClientProvider>
  );
}

it('separates pending data, a failed list and a successful empty retry', async () => {
  vi.mocked(listConversations).mockReturnValueOnce(new Promise(() => {}));
  renderPage();
  expect(screen.getByRole('status')).toHaveAttribute('aria-busy', 'true');
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
