import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mocks must precede the component import so its transitive imports resolve to
// the mocked exports at module-evaluation time.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (sel: (s: { activeBusinessId: string | null }) => unknown) =>
    sel({ activeBusinessId: 'biz-1' }),
}));

const post = vi.fn();
const put = vi.fn();
const get = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({ post, put, get }),
}));

import { toast } from 'sonner';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { FirstActionWizard } from '@/components/onboarding/FirstActionWizard';

interface ReviewRow {
  id: string;
  businessId: string;
  platform: string;
  externalId: string;
  authorName: string;
  rating: number;
  text: string;
  replyStatus: string;
  createdAt: string;
  draftReply?: string;
  draftStatus?: string;
}

function readyReview(over: Partial<ReviewRow> = {}): ReviewRow {
  return {
    id: 'r-1',
    businessId: 'biz-1',
    platform: 'yandex_business',
    externalId: 'ext-1',
    authorName: 'Иван',
    rating: 5,
    text: 'Отличный сервис!',
    replyStatus: 'pending',
    createdAt: '2026-07-01T10:00:00Z',
    draftReply: 'Спасибо за отзыв!',
    draftStatus: 'ready',
    ...over,
  };
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  post.mockReset();
  put.mockReset();
  get.mockReset();
  vi.mocked(toast.error).mockReset();
  post.mockResolvedValue({ data: { results: [], succeeded: 0 } });
  put.mockResolvedValue({ data: {} });
});

afterEach(() => {
  cleanup();
});

describe('FirstActionWizard — happy-path chain', () => {
  it('runs refresh -> batch-draft -> poll and publishes the freshest ready draft in one tap', async () => {
    get.mockResolvedValue({ data: [readyReview()] });

    render(
      <Wrapper>
        <FirstActionWizard open onClose={vi.fn()} />
      </Wrapper>
    );

    // The chain drives both write endpoints via the existing paths.
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/reviews/refresh', undefined, expect.any(Object))
    );
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/reviews/batch-draft', { reviewIds: [] })
    );

    // The freshest ready draft surfaces with its one-tap publish action.
    const draft = await screen.findByText('Спасибо за отзыв!');
    expect(draft).toBeInTheDocument();
    expect(screen.getByText('Отличный сервис!')).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Опубликовать' }));

    // Publish reuses the reviews reply endpoint (PUT /reviews/{id}/reply).
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/reviews/r-1/reply', { replyText: 'Спасибо за отзыв!' })
    );
    expect(await screen.findByText('Готово — ответ опубликован')).toBeInTheDocument();
  });

  it('picks the newest of several ready drafts', async () => {
    get.mockResolvedValue({
      data: [
        readyReview({ id: 'old', createdAt: '2026-06-01T10:00:00Z', draftReply: 'Старый' }),
        readyReview({ id: 'new', createdAt: '2026-07-04T10:00:00Z', draftReply: 'Новый' }),
      ],
    });

    render(
      <Wrapper>
        <FirstActionWizard open onClose={vi.fn()} />
      </Wrapper>
    );

    expect(await screen.findByText('Новый')).toBeInTheDocument();
    expect(screen.queryByText('Старый')).not.toBeInTheDocument();
  });
});

describe('FirstActionWizard — empty-backlog fallback', () => {
  it('a brand-new org with zero pending reviews shows the compose-in-chat CTA, not a draft', async () => {
    get.mockResolvedValue({ data: [] });

    render(
      <Wrapper>
        <FirstActionWizard open onClose={vi.fn()} />
      </Wrapper>
    );

    expect(await screen.findByText('Пока нет отзывов')).toBeInTheDocument();
    const cta = screen.getByRole('link', { name: 'Создать пост в чате' });
    expect(cta).toHaveAttribute('href', '/chat');
    // Never publishes and never shows a draft in the empty case.
    expect(put).not.toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Опубликовать' })).not.toBeInTheDocument();
  });

  it('does NOT read a fetch error as an empty backlog (isSuccess-gated, not isPlaceholderData)', async () => {
    get.mockRejectedValue(new Error('network'));

    render(
      <Wrapper>
        <FirstActionWizard open onClose={vi.fn()} />
      </Wrapper>
    );

    // On a fetch error the funnel must not claim "no reviews yet" — that would
    // mislead. It routes to the failed/retry state instead.
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/reviews/batch-draft', { reviewIds: [] })
    );
    await waitFor(() => expect(screen.queryByText('Пока нет отзывов')).not.toBeInTheDocument(), {
      timeout: 2000,
    });
  });
});

describe('FirstActionWizard — controlled + generating', () => {
  it('renders nothing interactive when closed (parent-controlled open)', () => {
    render(
      <Wrapper>
        <FirstActionWizard open={false} onClose={vi.fn()} />
      </Wrapper>
    );
    expect(screen.queryByText('Первое действие с ИИ')).not.toBeInTheDocument();
    // No chain runs while closed.
    expect(post).not.toHaveBeenCalled();
  });

  it('keeps a quiet spinner (no draft) while a draft is still generating', async () => {
    get.mockResolvedValue({ data: [readyReview({ draftStatus: 'generating', draftReply: '' })] });

    render(
      <Wrapper>
        <FirstActionWizard open onClose={vi.fn()} />
      </Wrapper>
    );

    expect(await screen.findByText('Готовим первое действие')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Опубликовать' })).not.toBeInTheDocument();
  });
});

function ProfileProbe({ fetchProfile }: { fetchProfile: () => Promise<boolean> }) {
  const { data } = useQuery({
    queryKey: QUERY_KEYS.BUSINESS_PROFILE('biz-1'),
    queryFn: fetchProfile,
    staleTime: Infinity,
  });
  return <output aria-label="Activation">{String(data)}</output>;
}

describe('FirstActionWizard profile refresh', () => {
  it.each([true, false])(
    'refreshes after a ready draft and only after a successful publish (%s)',
    async (publishSucceeds) => {
      const fetchProfile = vi.fn().mockResolvedValue(false);
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      await client.prefetchQuery({
        queryKey: QUERY_KEYS.BUSINESS_PROFILE('biz-1'),
        queryFn: fetchProfile,
        staleTime: Infinity,
      });
      fetchProfile.mockResolvedValue(true);
      get.mockResolvedValue({ data: [readyReview()] });
      if (!publishSucceeds) put.mockRejectedValue(new Error('publish failed'));

      render(
        <QueryClientProvider client={client}>
          <ProfileProbe fetchProfile={fetchProfile} />
          <FirstActionWizard open onClose={vi.fn()} />
        </QueryClientProvider>
      );

      await screen.findByText('Спасибо за отзыв!');
      await waitFor(() => expect(screen.getByLabelText('Activation')).toHaveTextContent('true'));
      expect(fetchProfile).toHaveBeenCalledTimes(2);
      await userEvent.setup().click(screen.getByRole('button', { name: 'Опубликовать' }));

      if (publishSucceeds) {
        await screen.findByText('Готово — ответ опубликован');
        await waitFor(() => expect(fetchProfile).toHaveBeenCalledTimes(3));
      } else {
        await waitFor(() => expect(toast.error).toHaveBeenCalled());
        expect(fetchProfile).toHaveBeenCalledTimes(2);
      }
      client.clear();
    }
  );

  it.each([
    [],
    [readyReview({ draftStatus: 'generating', draftReply: '' })],
    [readyReview({ draftStatus: 'failed', draftReply: '' })],
    [readyReview({ draftStatus: 'ready', draftReply: '  ' })],
  ])('does not refresh the profile without a successful draft (%j)', async (reviews) => {
    const fetchProfile = vi.fn().mockResolvedValue(false);
    get.mockResolvedValue({ data: reviews });
    render(
      <Wrapper>
        <ProfileProbe fetchProfile={fetchProfile} />
        <FirstActionWizard open onClose={vi.fn()} />
      </Wrapper>
    );
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/reviews/batch-draft', { reviewIds: [] })
    );
    await waitFor(() => expect(screen.getByLabelText('Activation')).toHaveTextContent('false'));
    expect(fetchProfile).toHaveBeenCalledTimes(1);
  });
});
