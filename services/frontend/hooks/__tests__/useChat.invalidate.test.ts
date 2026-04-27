import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import React from 'react';

import { useChat } from '../useChat';
import { useAuthStore } from '@/lib/auth';
import { mockSSEResponse, sseLine } from '@/test-utils/sse-mock';

// Phase 18 / TITLE-06 / D-10 — useChat invalidates ['conversations'] EXACTLY
// ONCE on chat SSE 'done'. PITFALLS §13: title arrival is OUT-OF-BAND from
// the chat stream — never muxed into the chat SSE event types.
//
// W-05 enforcement: the test exercises the hook through the SAME SSE
// consumption path production uses (fetch with a mocked streaming Response
// body — the hook does NOT use the global EventSource constructor). NO
// test-only export from useChat.ts (W-05 forbids the test-only escape
// hatch named in the plan). The fetch stream mock is the canonical pattern
// from `test-utils/sse-mock.ts`.

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

describe("useChat — D-10 invalidation on SSE 'done' (W-05 fetch-stream mock)", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({
      user: null,
      accessToken: 'test-token',
      isAuthenticated: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("invalidates ['conversations'] exactly once when chat SSE emits 'done'", async () => {
    const fetchMock = vi.fn();

    // 1) GET /messages — empty hydration.
    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL) => {
      expect(String(input)).toMatch(/\/api\/v1\/conversations\/.+\/messages$/);
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    // 2) POST /chat/{id} — SSE stream emits a partial text and then `done`.
    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL) => {
      expect(String(input)).toMatch(/\/api\/v1\/chat\/cid-d10$/);
      return mockSSEResponse([
        sseLine({ type: 'text', content: 'Hi ' }),
        sseLine({ type: 'done' }),
      ]);
    });

    vi.stubGlobal('fetch', fetchMock);

    // Spy on `invalidateQueries` of the QueryClient that the hook uses.
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');

    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(QueryClientProvider, { client: queryClient }, children);

    const { result } = renderHook(() => useChat('cid-d10'), { wrapper });

    // Wait for the hydration fetch to finish.
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Drive a `sendMessage` so the SSE consumer runs and emits `done`.
    await act(async () => {
      await result.current.sendMessage('hello');
    });

    // Exactly one invalidation against ['conversations'].
    const conversationsCalls = invalidateSpy.mock.calls.filter((c) => {
      const arg = c[0] as { queryKey?: unknown[] } | undefined;
      return Array.isArray(arg?.queryKey) && arg!.queryKey![0] === 'conversations';
    });
    expect(conversationsCalls).toHaveLength(1);
    expect(conversationsCalls[0][0]).toEqual({ queryKey: ['conversations'] });
  });

  it("does NOT invalidate ['conversations'] when SSE stream lacks a 'done' event (e.g., aborted stream)", async () => {
    const fetchMock = vi.fn();
    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    fetchMock.mockImplementationOnce(async () => {
      // Stream ends without a 'done' event (e.g., agent paused on tool_approval_required).
      return mockSSEResponse([sseLine({ type: 'text', content: 'partial...' })]);
    });

    vi.stubGlobal('fetch', fetchMock);

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');

    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(QueryClientProvider, { client: queryClient }, children);

    const { result } = renderHook(() => useChat('cid-no-done'), { wrapper });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      await result.current.sendMessage('hello');
    });

    const conversationsCalls = invalidateSpy.mock.calls.filter((c) => {
      const arg = c[0] as { queryKey?: unknown[] } | undefined;
      return Array.isArray(arg?.queryKey) && arg!.queryKey![0] === 'conversations';
    });
    expect(conversationsCalls).toHaveLength(0);
  });
});
