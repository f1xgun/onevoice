import { it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import ReviewsPage from '../page';

const put = vi.hoisted(() => vi.fn().mockResolvedValue({ data: {} }));
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: true }) }));
vi.mock('@/lib/stores/business', () => ({ useBusinessStore: () => 'organization' }));
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: () =>
      Promise.resolve({
        data: [
          {
            id: 'failed',
            platform: 'telegram',
            authorName: 'Reviewer',
            text: 'Review',
            replyStatus: 'error',
            replyText: 'Failed attempt',
            createdAt: '2026-09-06T10:00:00Z',
          },
        ],
      }),
    put,
  }),
}));

it('lets the owner edit and send a failed reply', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ReviewsPage />
    </QueryClientProvider>
  );
  fireEvent.click(await screen.findByRole('button', { name: 'Отредактировать' }));
  const field = screen.getByRole('textbox');
  expect(field).toHaveValue('Failed attempt');
  fireEvent.change(field, { target: { value: 'Corrected reply' } });
  fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(expect.any(String), { replyText: 'Corrected reply' })
  );
});
