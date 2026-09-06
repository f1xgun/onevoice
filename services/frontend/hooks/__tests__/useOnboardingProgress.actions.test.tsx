import { describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { conversationsQueryKey } from '@/hooks/useConversations';
import { useOnboardingProgress } from '../useOnboardingProgress';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (select: (s: { activeBusinessId: string }) => unknown) =>
    select({ activeBusinessId: 'org' }),
}));
vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => ({ data: [{ id: 'org' }], isSuccess: true }),
}));
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: false }) }));
vi.mock('@/lib/hooks/useMembers', () => ({ useMembers: () => ({ data: [] }) }));
const { get } = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock('@/lib/api/business-api', () => ({ bizApi: () => ({ get }) }));

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('first action completion', () => {
  it.each([
    [[], false],
    [[{ status: 'error' }], false],
    [[{ status: 'running' }], false],
    [[{ status: 'done' }], true],
  ])('derives completion from successful tasks: %j', async (tasks, done) => {
    get.mockImplementation(async (path: string) => ({ data: path === '/tasks' ? tasks : [] }));
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.loading).toBe(false)
    );
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(done);
  });

  it.each([
    ['complete', 'Готовый текст публикации', true],
    [undefined, 'Сохранённый ответ', true],
    ['in_progress', 'Начало ответа', false],
    ['pending_approval', 'Ожидание подтверждения', false],
    ['complete', '   ', false],
  ])('checks persisted text-only replies (%s)', async (status, content, done) => {
    get.mockImplementation(async (path: string) => {
      if (path === '/conversations') return { data: [{ id: 'text-chat' }] };
      if (path === '/conversations/text-chat/messages')
        return {
          data: {
            messages: [
              { id: 'user', role: 'user', content: 'Напиши текст публикации' },
              { id: 'reply', role: 'assistant', content, status },
            ],
            pendingApprovals: [],
          },
        };
      return { data: [] };
    });
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.loading).toBe(false)
    );
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(done);
    expect(get).toHaveBeenCalledWith(
      '/conversations/text-chat/messages',
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    );
  });

  it('updates after a text-only turn is persisted and conversations are invalidated', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    let answered = false;
    get.mockImplementation(async (path: string) => ({
      data:
        path === '/conversations'
          ? [{ id: 'text-chat' }]
          : path.endsWith('/messages')
            ? {
                messages: answered
                  ? [
                      {
                        id: 'reply',
                        role: 'assistant',
                        content: 'Готовый текст',
                        status: 'complete',
                        toolCalls: [],
                      },
                    ]
                  : [{ id: 'request', role: 'user', content: 'Напиши текст' }],
              }
            : [],
    }));
    function Provider({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    }
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Provider });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.loading).toBe(false)
    );
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(false);
    answered = true;
    await act(async () => {
      await client.invalidateQueries({ queryKey: conversationsQueryKey('org') });
    });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(true)
    );
  });

  it('does not count a conversation with only a user request', async () => {
    get.mockImplementation(async (path: string) => ({
      data:
        path === '/conversations'
          ? [{ id: 'empty' }]
          : path.endsWith('/messages')
            ? { messages: [{ role: 'user', content: 'Напиши текст' }] }
            : [],
    }));
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.loading).toBe(false)
    );
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(false);
  });

  it('does not mark a failed task request complete', async () => {
    get.mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.loading).toBe(false)
    );
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(false);
  });
});
