// Regression: switching to a DIFFERENT conversation mid-stream must abort the
// orphaned stream and LOAD the new conversation's history.
//
// Navigation between conversations is router.push('/chat/${id}') / <Link> on the
// SAME /chat/[id] dynamic route, so the App Router REUSES the ChatWindow /
// useConversationFlow instance (no remount, the unmount-abort effect never
// fires). A send started for conversation A keeps isStreamingRef true (it is
// only reset in sendMessage's finally). When conversationId flips to B the load
// effect re-runs with isInitial=true.
//
// The streaming guard must short-circuit ONLY for the SAME conversation that is
// streaming. A guard scoped to ALL initial-load re-runs (`if (isStreamingRef.current)`)
// short-circuits B's load too — B never loads and stays stuck showing A. The
// scoped fix records the streaming conversation id and, on a real switch, aborts
// the old send and falls through to load B.
//
// Fail-on-revert: replace the scoped guard with the unscoped
// `if (isStreamingRef.current)` (and drop the switch-abort) and this test fails
// (B's history is not loaded; A's message lingers).

import { type ReactNode, createElement } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useConversationFlow } from '../useConversationFlow';
import { useAuthStore } from '@/lib/auth';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// Fixed active business — this scenario varies conversationId, not the business.
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-a' }),
}));

function makeQCWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 0 } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

// A controllable SSE response: stays open until close() is called, keeping
// isStreamingRef true across the conversation switch so the guard is exercised
// mid-stream deterministically.
function gatedSSEResponse() {
  const encoder = new TextEncoder();
  let controller: ReadableStreamDefaultController<Uint8Array>;
  const stream = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
  });
  const response = new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
  return {
    response,
    push(event: Record<string, unknown>) {
      controller.enqueue(encoder.encode('data: ' + JSON.stringify(event) + '\n'));
    },
    close() {
      controller.close();
    },
  };
}

describe('useConversationFlow — switching conversations mid-stream loads the new one', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({ user: null, accessToken: 'test-token', isAuthenticated: true });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('aborts the in-flight stream for A and loads B history when conversationId flips mid-stream', async () => {
    const convAInitial = { messages: [], pendingApprovals: [] };
    const convBHistory = {
      messages: [
        { id: 'b-u1', role: 'user', content: 'B prompt', toolCalls: [] },
        { id: 'b-a1', role: 'assistant', content: 'B reply', toolCalls: [] },
      ],
      pendingApprovals: [],
    };

    const gate = gatedSSEResponse();
    let aborted = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes('/chat/')) {
        // Observe an abort of the send stream (conversation switch should fire it).
        init?.signal?.addEventListener('abort', () => {
          aborted = true;
        });
        return gate.response;
      }
      // History GETs: cid-A returns empty; cid-B returns its persisted thread.
      return jsonResponse(url.includes('cid-B') ? convBHistory : convAInitial);
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result, rerender } = renderHook(
      ({ conversationId }: { conversationId: string }) => useConversationFlow({ conversationId }),
      { wrapper: makeQCWrapper(), initialProps: { conversationId: 'cid-A' } }
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Start a real send on A — isStreamingRef flips true synchronously.
    let sendPromise!: Promise<void>;
    await act(async () => {
      sendPromise = result.current.sendMessage('A prompt');
    });
    expect(result.current.isStreaming).toBe(true);

    // Stream partial content for A while the turn is still open.
    await act(async () => {
      gate.push({ type: 'text', content: 'A-streamed' });
      await Promise.resolve();
    });
    expect(result.current.messages.some((m) => m.content === 'A-streamed')).toBe(true);

    // Switch to conversation B mid-stream (same dynamic route → no remount).
    await act(async () => {
      rerender({ conversationId: 'cid-B' });
      await new Promise((r) => setTimeout(r, 30));
    });

    // The orphaned A stream was aborted and streaming state cleared.
    expect(aborted).toBe(true);
    expect(result.current.isStreaming).toBe(false);

    // B's history loaded — and A's streamed turn is no longer shown.
    await waitFor(() => {
      expect(result.current.messages.some((m) => m.id === 'b-a1')).toBe(true);
    });
    const msgs = result.current.messages;
    expect(msgs.some((m) => m.content === 'B reply')).toBe(true);
    expect(msgs.some((m) => m.content === 'B prompt')).toBe(true);
    expect(msgs.some((m) => m.content === 'A-streamed')).toBe(false);
    expect(msgs.some((m) => m.content === 'A prompt')).toBe(false);
    expect(result.current.isLoading).toBe(false);

    // Settle the orphaned A stream (already aborted) without leaking.
    await act(async () => {
      gate.close();
      await sendPromise;
    });
  });
});
