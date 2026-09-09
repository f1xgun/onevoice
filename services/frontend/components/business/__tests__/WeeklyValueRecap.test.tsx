import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { WeeklyValueRecap, weeklyRecapDismissKey } from '../WeeklyValueRecap';
import { trackEvent } from '@/lib/telemetry';

const get = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (businessId: string) => ({ get: (path: string) => get(path, businessId) }),
}));
vi.mock('@/lib/telemetry', () => ({ trackEvent: vi.fn() }));
vi.mock('next-intl', () => ({
  useLocale: () => 'en',
  useTranslations: () => (key: string, values?: Record<string, number | string>) => {
    if (key === 'title') return 'Your week in OneVoice';
    if (key === 'period') return `Completed operations, ${values?.from}–${values?.to}`;
    if (key === 'posts') return `${values?.count} posts published`;
    if (key === 'replies') return `${values?.count} review replies sent`;
    if (key === 'syncs') return `${values?.count} profile syncs completed`;
    if (key === 'dismiss') return 'Dismiss weekly recap';
    return 'Recorded completed operations';
  },
}));
vi.mock('@/lib/auth', () => ({
  useAuthStore: (pick: (s: object) => unknown) => pick({ user: { id: 'user-1' } }),
}));
let activeBusinessId = 'biz-1';
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (pick: (s: object) => unknown) => pick({ activeBusinessId }),
}));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false, isError: false, refetch: vi.fn() }),
}));

function renderRecap() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <WeeklyValueRecap />
    </QueryClientProvider>
  );
}

describe('WeeklyValueRecap', () => {
  beforeEach(() => {
    activeBusinessId = 'biz-1';
    localStorage.clear();
    get.mockReset();
    vi.mocked(trackEvent).mockClear();
  });

  it('does not flash a cached recap while resolving a dismissed organization key', async () => {
    const recap = {
      weekStart: '2026-08-17T00:00:00Z',
      weekEnd: '2026-08-24T00:00:00Z',
      publishedPosts: 2,
      dispatchedReviewReplies: 0,
      completedSyncs: 0,
    };
    get.mockResolvedValue({ status: 200, data: recap });
    const view = renderRecap();
    expect(await screen.findByText('2 posts published')).toBeInTheDocument();
    localStorage.setItem(weeklyRecapDismissKey('user-1', 'biz-2', recap.weekStart), '1');
    activeBusinessId = 'biz-2';
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <WeeklyValueRecap />
      </QueryClientProvider>
    );
    expect(screen.queryByText('2 posts published')).not.toBeInTheDocument();
    await waitFor(() => expect(get).toHaveBeenCalledWith('/recap/latest', 'biz-2'));
    expect(screen.queryByText('2 posts published')).not.toBeInTheDocument();
  });

  it('shows only nonzero categories, tracks display, and persists a scoped dismissal', async () => {
    get.mockResolvedValue({
      data: {
        weekStart: '2026-08-31T00:00:00Z',
        weekEnd: '2026-09-07T00:00:00Z',
        publishedPosts: 2,
        dispatchedReviewReplies: 0,
        completedSyncs: 1,
      },
    });
    renderRecap();
    expect(await screen.findByText('2 posts published')).toBeInTheDocument();
    expect(screen.queryByText(/review replies/)).not.toBeInTheDocument();
    expect(trackEvent).toHaveBeenCalledWith('value_recap', 'shown', {
      page: '/business',
      businessId: 'biz-1',
    });
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss weekly recap' }));
    expect(
      localStorage.getItem(weeklyRecapDismissKey('user-1', 'biz-1', '2026-08-31T00:00:00Z'))
    ).toBe('1');
    await waitFor(() => expect(screen.queryByText('2 posts published')).not.toBeInTheDocument());
    expect(trackEvent).toHaveBeenCalledWith('value_recap', 'dismissed', {
      page: '/business',
      businessId: 'biz-1',
    });
  });

  it('renders nothing when the endpoint has no eligible week', async () => {
    get.mockResolvedValue({ status: 204, data: '' });
    renderRecap();
    await waitFor(() => expect(get).toHaveBeenCalled());
    expect(screen.queryByText('Your week in OneVoice')).not.toBeInTheDocument();
  });

  it('rejects malformed or client-manipulated low-count responses', async () => {
    get.mockResolvedValue({
      status: 200,
      data: {
        weekStart: 'bad-date',
        weekEnd: '2026-09-07T00:00:00Z',
        publishedPosts: -1,
        dispatchedReviewReplies: 0,
        completedSyncs: 0,
      },
    });
    renderRecap();
    await waitFor(() => expect(get).toHaveBeenCalled());
    expect(screen.queryByText('Your week in OneVoice')).not.toBeInTheDocument();
  });

  it('keeps dismissal in memory when storage access is denied', async () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied');
    });
    get.mockResolvedValue({
      status: 200,
      data: {
        weekStart: '2026-08-24T00:00:00Z',
        weekEnd: '2026-08-31T00:00:00Z',
        publishedPosts: 2,
        dispatchedReviewReplies: 0,
        completedSyncs: 0,
      },
    });
    renderRecap();
    expect(await screen.findByText('2 posts published')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss weekly recap' }));
    await waitFor(() => expect(screen.queryByText('2 posts published')).not.toBeInTheDocument());
    getItem.mockRestore();
    setItem.mockRestore();
  });
});
