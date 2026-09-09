import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useBusinessStore } from '@/lib/stores/business';
import ReviewsPage from '../page';

const get = vi.hoisted(() => vi.fn());
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: true }) }));
vi.mock('@/lib/api/business-api', () => ({ bizApi: () => ({ get }) }));

const review = {
  id: 'review-a',
  businessId: 'business-a',
  platform: 'vk',
  authorName: 'Мария Иванова',
  text: 'Отзыв организации А',
  replyStatus: 'replied',
  createdAt: '2026-09-09T07:00:00Z',
};

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ReviewsPage />
    </QueryClientProvider>
  );
}

beforeEach(() => {
  useBusinessStore.setState({ activeBusinessId: 'business-a' });
  get.mockReset().mockResolvedValue({ data: [review] });
});
afterEach(async () => {
  await act(async () => useBusinessStore.setState({ activeBusinessId: null }));
});

it('keeps reviews visible during a filter request, then offers to reset an empty selection', async () => {
  let completeFilter!: (result: { data: object[] }) => void;
  get.mockImplementation((path: string) =>
    path.includes('reply_status=pending')
      ? new Promise((resolve) => {
          completeFilter = resolve;
        })
      : Promise.resolve({ data: [review] })
  );
  renderPage();
  await screen.findByText(review.text);
  fireEvent.click(screen.getByRole('radio', { name: 'Без ответа' }));
  expect(await screen.findByText('Обновляем список…')).toBeInTheDocument();
  expect(screen.getByText(review.text)).toBeInTheDocument();
  expect(screen.queryByText('Открытых отзывов нет')).not.toBeInTheDocument();
  await act(async () => completeFilter({ data: [] }));
  const reset = await screen.findByRole('button', { name: 'Сбросить фильтры' });
  fireEvent.click(reset);
  expect(await screen.findByText(review.text)).toBeInTheDocument();
});

it('never retains another organization reviews during the next organization load', async () => {
  renderPage();
  await screen.findByText(review.text);
  get.mockImplementation(() => new Promise(() => {}));
  await act(async () => useBusinessStore.setState({ activeBusinessId: 'business-b' }));
  await waitFor(() => expect(screen.queryByText(review.text)).not.toBeInTheDocument());
});
