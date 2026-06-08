import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import React from 'react';
import { useConversationFlow } from '../useConversationFlow';
import { useAuthStore } from '@/lib/auth';

// Covers the "reload mid-turn" recovery: the server finishes and persists a
// turn even after the client disconnects, so on reload a trailing user message
// with no assistant reply means "still generating". The hook shows a typing
// placeholder, sets awaitingTurn, and polls GET /messages until the reply
// lands. See useConversationFlow load effect.

const SYNTHETIC_ID = '__onevoice_awaiting_turn__';
const POLL_INTERVAL_MS = 3000;

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-test' }),
}));

function makeQCWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('useConversationFlow — awaiting in-flight turn after reload', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.useFakeTimers();
    useAuthStore.setState({ user: null, accessToken: 'test-token', isAuthenticated: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('shows a typing placeholder + awaitingTurn, then resolves on the next poll', async () => {
    const userMsg = { id: 'u1', role: 'user', content: 'Post to telegram', toolCalls: [] };
    const assistantMsg = { id: 'a1', role: 'assistant', content: 'Posted!', toolCalls: [] };

    const fetchMock = vi
      .fn()
      .mockImplementationOnce(async () =>
        jsonResponse({ messages: [userMsg], pendingApprovals: [] })
      )
      .mockImplementationOnce(async () =>
        jsonResponse({ messages: [userMsg, assistantMsg], pendingApprovals: [] })
      );
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useConversationFlow({ conversationId: 'cid-await-1' }), {
      wrapper: makeQCWrapper(),
    });

    // Initial load resolves → placeholder injected, awaitingTurn true.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.isLoading).toBe(false);
    expect(result.current.awaitingTurn).toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const mid = result.current.messages;
    expect(mid[mid.length - 1]).toMatchObject({
      id: SYNTHETIC_ID,
      role: 'assistant',
      status: 'streaming',
    });

    // The scheduled poll fires → assistant reply lands, placeholder removed.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(result.current.awaitingTurn).toBe(false);
    const final = result.current.messages;
    expect(final.some((m) => m.id === SYNTHETIC_ID)).toBe(false);
    expect(final.some((m) => m.content === 'Posted!')).toBe(true);
  });

  it('does not poll when the conversation already ends with an assistant reply', async () => {
    const done = {
      messages: [
        { id: 'u1', role: 'user', content: 'hi', toolCalls: [] },
        { id: 'a1', role: 'assistant', content: 'hello', toolCalls: [] },
      ],
      pendingApprovals: [],
    };
    const fetchMock = vi.fn().mockImplementation(async () => jsonResponse(done));
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useConversationFlow({ conversationId: 'cid-done' }), {
      wrapper: makeQCWrapper(),
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.awaitingTurn).toBe(false);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2);
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
