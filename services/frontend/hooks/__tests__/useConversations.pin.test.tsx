import { describe, expect, it, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import React, { type ReactNode } from 'react';
import { usePinConversation, useUnpinConversation } from '../useConversations';

const apiPost = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    post: (url: string, body?: unknown) => apiPost(url, body),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) =>
    React.createElement(QueryClientProvider, { value: qc }, children);
  return { qc, wrapper };
}

describe('usePinConversation / useUnpinConversation — Phase 19 / Plan 19-02', () => {
  beforeEach(() => {
    apiPost.mockReset();
  });

  it('usePinConversation calls POST /conversations/{id}/pin', async () => {
    apiPost.mockResolvedValue({
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
    const { qc, wrapper } = setup();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => usePinConversation(), {
      wrapper: ({ children }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>,
    });

    await act(async () => {
      await result.current.mutateAsync('c-1');
    });

    expect(apiPost).toHaveBeenCalledWith('/conversations/c-1/pin', undefined);
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['conversations'] })
    );
    // wrapper not directly used, but kept available for parity with other tests.
    void wrapper;
  });

  it('useUnpinConversation calls POST /conversations/{id}/unpin', async () => {
    apiPost.mockResolvedValue({
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

    expect(apiPost).toHaveBeenCalledWith('/conversations/c-2/unpin', undefined);
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['conversations'] })
    );
  });
});
