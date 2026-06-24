import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import React from 'react';
import { useConversationFlow } from '../useConversationFlow';
import { useAuthStore } from '@/lib/auth';

// Covers the unmount-cleanup effect: a send/resume stream left in-flight when
// the chat component unmounts must abort its AbortController so the detached
// fetch stops invoking onEvent (setState) on an unmounted component.

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

// neverEndingStream returns a Response whose body never closes, so
// consumeSSEStream parks on reader.read() and the send stays in-flight.
function neverEndingStream(): Response {
  const stream = new ReadableStream<Uint8Array>({
    start() {},
  });
  return new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
}

describe('useConversationFlow — abort in-flight stream on unmount', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({ user: null, accessToken: 'test-token', isAuthenticated: true });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('aborts the send stream signal when the component unmounts mid-stream', async () => {
    let capturedSignal: AbortSignal | undefined;
    const fetchMock = vi.fn().mockImplementation(async (_input: unknown, init?: RequestInit) => {
      if (init?.method === 'POST') {
        capturedSignal = init.signal ?? undefined;
        return neverEndingStream();
      }
      return jsonResponse({ messages: [], pendingApprovals: [] });
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result, unmount } = renderHook(
      () => useConversationFlow({ conversationId: 'cid-unmount' }),
      { wrapper: makeQCWrapper() }
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      void result.current.sendMessage('hello');
    });

    await waitFor(() => {
      expect(capturedSignal).toBeDefined();
      expect(result.current.isStreaming).toBe(true);
    });
    expect(capturedSignal!.aborted).toBe(false);

    unmount();

    expect(capturedSignal!.aborted).toBe(true);
  });
});
