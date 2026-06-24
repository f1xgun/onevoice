// Regression: a re-run of the history-load effect must NEVER clobber a live
// in-flight stream. The load effect can re-fire mid-stream when one of its
// dependencies changes (here we drive it by flipping activeBusinessId, which
// re-runs the effect with isInitial=true). The persisted GET /messages does
// NOT yet contain the in-progress turn — the server persists only after the
// stream ends — so replacing the live message array drops the streamed text
// and tool calls and freezes the chat.
//
// The fix is the streaming guard in the load effect: `if (isStreamingRef.current)`
// short-circuits BOTH initial and poll re-runs. Reverting the guard to the old
// `!isInitial && isStreamingRef.current` scope clobbers the stream → this test
// fails (the asserted streamed content disappears).

import { createElement, type ReactNode } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useConversationFlow } from '../useConversationFlow';
import { useAuthStore } from '@/lib/auth';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// Mutable active-business id so a test can flip it mid-stream and force the
// load effect (which depends on activeBusinessId) to re-run.
let activeBusinessId: string | null = 'biz-a';
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId }),
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

// A controllable SSE response: each pushed string is enqueued, and the stream
// stays open until close() is called. This keeps isStreamingRef true across the
// load-effect re-run so the guard is exercised mid-stream deterministically.
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

describe('useConversationFlow — live stream survives a load-effect re-run', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    activeBusinessId = 'biz-a';
    useAuthStore.setState({ user: null, accessToken: 'test-token', isAuthenticated: true });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('does not replace the streamed assistant message when the load effect re-fires mid-stream', async () => {
    // The mid-stream history GET persists only the prior, completed turn — NOT
    // the in-progress one. A clobber would replace the live array with just
    // this stale list (a single old user message, no assistant reply).
    const initialEmpty = { messages: [], pendingApprovals: [] };
    const persistedStale = {
      messages: [{ id: 'u-old', role: 'user', content: 'earlier prompt', toolCalls: [] }],
      pendingApprovals: [],
    };

    const gate = gatedSSEResponse();
    let getCalls = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/chat/')) return gate.response;
      getCalls += 1;
      return jsonResponse(getCalls === 1 ? initialEmpty : persistedStale);
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result, rerender } = renderHook(
      () => useConversationFlow({ conversationId: 'cid-clobber-1' }),
      { wrapper: makeQCWrapper() }
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Start a real send — isStreamingRef flips true synchronously.
    let sendPromise!: Promise<void>;
    await act(async () => {
      sendPromise = result.current.sendMessage('new prompt');
    });
    expect(result.current.isStreaming).toBe(true);

    // Stream partial content while the turn is still open.
    await act(async () => {
      gate.push({ type: 'text', content: 'streamed-' });
      gate.push({ type: 'text', content: 'reply' });
      gate.push({
        type: 'tool_call',
        tool_call_id: 'tc-1',
        tool_name: 'telegram__send_channel_post',
        tool_args: { text: 'hi' },
      });
      await Promise.resolve();
    });

    // Force the history-load effect to re-run mid-stream by changing a
    // dependency. With the bug, this initial-load re-run reaches setMessages and
    // clobbers the live array with the persisted single-message list.
    activeBusinessId = 'biz-b';
    await act(async () => {
      rerender();
      await new Promise((r) => setTimeout(r, 30));
    });
    // The re-run did fire its history GET (proving the effect re-ran); the
    // streaming guard is what prevents the clobber, not a skipped fetch.
    const ranAgain = fetchMock.mock.calls.some((c) => String(c[0]).includes('biz-b'));
    expect(ranAgain).toBe(true);

    // The streamed turn must still be intact even though the load re-ran.
    let assistant = result.current.messages.find((m) => m.role === 'assistant');
    expect(assistant).toBeDefined();
    expect(assistant!.content).toBe('streamed-reply');
    expect(assistant!.toolCalls?.some((t) => t.id === 'tc-1')).toBe(true);

    // Close the stream and let the send settle.
    await act(async () => {
      gate.push({ type: 'done' });
      gate.close();
      await sendPromise;
    });

    const msgs = result.current.messages;
    assistant = msgs.find((m) => m.role === 'assistant');
    expect(assistant!.content).toBe('streamed-reply');
    expect(assistant!.toolCalls?.some((t) => t.id === 'tc-1')).toBe(true);
    expect(msgs.some((m) => m.content === 'new prompt')).toBe(true);
    // The persisted-only list (single stale user message) never took over.
    expect(msgs.some((m) => m.id === 'u-old')).toBe(false);
  });
});
