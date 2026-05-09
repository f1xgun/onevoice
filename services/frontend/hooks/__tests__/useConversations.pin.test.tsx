import { describe, expect, it, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import React, { type ReactNode } from 'react';
import { usePinConversation, useUnpinConversation } from '../useConversations';

// Mock bizApi so calls are intercepted without a live server.
// conversations.ts now uses bizApi(activeBusinessId).post(...)
const bizApiPost = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: vi.fn(),
    post: (path: string, data?: unknown) => bizApiPost(bizId, path, data),
    put: vi.fn(),
    delete: vi.fn(),
  }),
}));

// Mock useBusinessStore so hooks can read activeBusinessId without localStorage.
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'test-biz-id' }),
}));

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) =>
    React.createElement(QueryClientProvider, { value: qc }, children);
  return { qc, wrapper };
}

describe('usePinConversation / useUnpinConversation — Phase 19 / Plan 19-02', () => {
  beforeEach(() => {
    bizApiPost.mockReset();
  });

  it('usePinConversation calls POST /conversations/{id}/pin via bizApi', async () => {
    bizApiPost.mockResolvedValue({
      data: {
        id: 'c-1',
        userId: 'u',
        businessId: 'b',
        projectId: null,
        title: 't',
        pinnedAt: '2026-04-27T12:00:00Z',
        createdAt: '',
        updatedAt: '',
      },
    });
    const { qc } = setup();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => usePinConversation(), {
      wrapper: ({ children }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>,
    });

    await act(async () => {
      await result.current.mutateAsync('c-1');
    });

    expect(bizApiPost).toHaveBeenCalledWith('test-biz-id', '/conversations/c-1/pin', undefined);
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ['businesses', 'test-biz-id', 'conversations'],
      })
    );
  });

  it('useUnpinConversation calls POST /conversations/{id}/unpin via bizApi', async () => {
    bizApiPost.mockResolvedValue({
      data: {
        id: 'c-2',
        userId: 'u',
        businessId: 'b',
        projectId: null,
        title: 't',
        pinnedAt: null,
        createdAt: '',
        updatedAt: '',
      },
    });
    const { qc } = setup();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useUnpinConversation(), {
      wrapper: ({ children }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>,
    });

    await act(async () => {
      await result.current.mutateAsync('c-2');
    });

    expect(bizApiPost).toHaveBeenCalledWith('test-biz-id', '/conversations/c-2/unpin', undefined);
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ['businesses', 'test-biz-id', 'conversations'],
      })
    );
  });
});
