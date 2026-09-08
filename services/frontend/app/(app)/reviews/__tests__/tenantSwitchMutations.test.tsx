import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { api } from '@/lib/api';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';

import ReviewsPage from '../page';

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess, error: vi.fn() } }));
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: true }) }));

const originalAdapter = api.defaults.adapter;
const sla = {
  total: 1,
  unanswered: 1,
  answered: 0,
  buckets: { lt24h: 1, h24to72: 0, gt72h: 0 },
  targetHours: 24,
  medianResponseHours: 0,
  averageResponseHours: 0,
  measuredResponses: 0,
  percentAnsweredWithinTarget: 0,
  oldestUnansweredHours: 1,
  platforms: [],
};

function review(businessId: string) {
  return {
    id: `review-${businessId}`,
    businessId,
    platform: 'telegram',
    authorName: `Author ${businessId}`,
    text: `Review ${businessId}`,
    replyStatus: 'pending',
    draftStatus: 'failed',
    createdAt: '2026-09-06T10:00:00Z',
  };
}

beforeEach(() => {
  toastSuccess.mockClear();
  useBusinessStore.setState({ activeBusinessId: 'business-a' });
});

afterEach(() => {
  api.defaults.adapter = originalAdapter;
  useBusinessStore.setState({ activeBusinessId: null });
});

it('keeps a new-business dialog open when an old-business reply completes late', async () => {
  let finishBusinessA!: () => void;
  api.defaults.adapter = async (config) => {
    if (config.method === 'put' && config.url?.includes('/businesses/business-a/')) {
      await new Promise<void>((resolve) => {
        finishBusinessA = resolve;
      });
    }
    const businessId = config.url?.includes('/businesses/business-b/')
      ? 'business-b'
      : 'business-a';
    return {
      data: config.url?.endsWith('/reviews/sla')
        ? sla
        : config.method === 'get'
          ? [review(businessId)]
          : {},
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    };
  };

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  render(
    <QueryClientProvider client={client}>
      <ReviewsPage />
    </QueryClientProvider>
  );

  await screen.findByText('Review business-a');
  fireEvent.click(screen.getByRole('button', { name: 'Написать ответ' }));
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Reply from A' } });
  fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
  await waitFor(() => expect(finishBusinessA).toBeDefined());

  await act(async () => useBusinessStore.setState({ activeBusinessId: 'business-b' }));
  fireEvent.click(screen.getByRole('button', { name: 'Отмена' }));
  await screen.findByText('Review business-b');
  fireEvent.click(screen.getByRole('button', { name: 'Написать ответ' }));
  expect(screen.getByRole('dialog')).toHaveTextContent('Author business-b');

  await act(async () => finishBusinessA());

  await waitFor(() =>
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: QUERY_KEYS.BUSINESS_REVIEWS('business-a'),
    })
  );
  expect(invalidate).not.toHaveBeenCalledWith({
    queryKey: QUERY_KEYS.BUSINESS_REVIEWS('business-b'),
  });
  expect(screen.getByRole('dialog')).toHaveTextContent('Author business-b');
  expect(toastSuccess).not.toHaveBeenCalled();
});
