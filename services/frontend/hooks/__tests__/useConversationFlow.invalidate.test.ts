import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import React from 'react';

import { useConversationFlow } from '../useConversationFlow';
import { useAuthStore } from '@/lib/auth';
import { mockSSEResponse, sseLine } from '@/test-utils/sse-mock';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-test' }),
}));

// useConversationFlow invalidates ['conversations'] EXACTLY ONCE on chat
// SSE 'done'. Title arrival is OUT-OF-BAND from the chat stream — never
// muxed into the chat SSE event types.
//
// The test exercises the hook through the SAME SSE consumption path
// production uses (fetch with a mocked streaming Response body — the hook
// does NOT use the global EventSource constructor). NO test-only export
// from useConversationFlow.ts (the test-only escape hatch is forbidden).
// The fetch stream mock is the canonical pattern from `test-utils/sse-mock.ts`.

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

describe("useConversationFlow — invalidation on SSE 'done' (fetch-stream mock)", () => {
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
    let streamController: ReadableStreamDefaultController<Uint8Array>;

    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL) => {
      expect(String(input)).toMatch(
        /\/api\/v1\/businesses\/biz-test\/conversations\/.+\/messages$/
      );
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL) => {
      expect(String(input)).toMatch(/\/api\/v1\/businesses\/biz-test\/chat\/cid-d10$/);
      return new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            streamController = controller;
            controller.enqueue(
              new TextEncoder().encode(
                sseLine({ type: 'text', content: 'Hi ' }) + sseLine({ type: 'done' })
              )
            );
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } }
      );
    });

    vi.stubGlobal('fetch', fetchMock);

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');

    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(QueryClientProvider, { client: queryClient }, children);

    const { result } = renderHook(() => useConversationFlow({ conversationId: 'cid-d10' }), {
      wrapper,
    });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    let send: Promise<void>;
    await act(async () => {
      send = result.current.sendMessage('hello');
    });
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['businesses', 'biz-test', 'tasks'] })
    );
    expect(invalidateSpy).not.toHaveBeenCalledWith({
      queryKey: ['businesses', 'biz-test', 'business'],
    });
    await act(async () => {
      streamController.close();
      await send;
    });

    const conversationsCalls = invalidateSpy.mock.calls.filter((c) => {
      const arg = c[0] as { queryKey?: unknown[] } | undefined;
      return (
        Array.isArray(arg?.queryKey) &&
        arg!.queryKey![0] === 'businesses' &&
        arg!.queryKey![2] === 'conversations'
      );
    });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['businesses', 'biz-test', 'tasks'] });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['businesses', 'biz-test', 'business'],
    });
    expect(conversationsCalls).toHaveLength(1);
    expect(conversationsCalls[0][0]).toEqual({
      queryKey: ['businesses', 'biz-test', 'conversations'],
    });
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
      return mockSSEResponse([sseLine({ type: 'text', content: 'partial...' })]);
    });

    vi.stubGlobal('fetch', fetchMock);

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');

    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(QueryClientProvider, { client: queryClient }, children);

    const { result } = renderHook(() => useConversationFlow({ conversationId: 'cid-no-done' }), {
      wrapper,
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      await result.current.sendMessage('hello');
    });

    const conversationsCalls = invalidateSpy.mock.calls.filter((c) => {
      const arg = c[0] as { queryKey?: unknown[] } | undefined;
      return (
        Array.isArray(arg?.queryKey) &&
        arg!.queryKey![0] === 'businesses' &&
        arg!.queryKey![2] === 'conversations'
      );
    });
    expect(conversationsCalls).toHaveLength(0);
  });
});
