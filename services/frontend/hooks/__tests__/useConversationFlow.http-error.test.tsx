// Regression: the chat POST can fail with a typed pre-stream error BEFORE the
// SSE body opens — e.g. HTTP 429 { code: "sse_concurrency_exceeded",
// retry_after_s } with a Retry-After header, or 503 rate_limit_unavailable,
// 404 business_not_found, 502 orchestrator_unavailable, 400. sendMessage used
// to pass the raw Response straight to consumeSSEStream, which throws a bare
// "HTTP <status>"; the catch only special-cased AbortError, so every typed
// error collapsed to the generic connectionError and the code / Retry-After
// were lost.
//
// The fix inspects `response.ok` before streaming, maps the status + body
// through mapPreStreamChatError, and stamps the same `error` SSE shape the
// renderer localizes (chat.streamError.*). Reverting the `!response.ok`
// handling makes consumeSSEStream throw and the bubble shows the generic
// connectionError → these assertions fail.

import { createElement, type ReactNode } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, render, screen, cleanup } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useConversationFlow } from '../useConversationFlow';
import { MessageBubble } from '@/components/chat/MessageBubble';
import { useAuthStore } from '@/lib/auth';

const toastError = vi.fn();
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: (...a: unknown[]) => toastError(...a) },
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-test' }),
}));

// chat.streamError.* literals in the default (ru) test locale.
const CONCURRENCY_COPY =
  'Уже идёт другой запрос в этом аккаунте. Дождитесь его завершения и повторите.';
const CONNECTION_ERROR_COPY = 'Ошибка соединения';

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

describe('useConversationFlow — typed pre-stream chat-POST errors', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    toastError.mockReset();
    useAuthStore.setState({ user: null, accessToken: 'test-token', isAuthenticated: true });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('maps a 429 sse_concurrency_exceeded POST to the specific localized notice, not connectionError', async () => {
    const concurrency429 = () =>
      new Response(JSON.stringify({ code: 'sse_concurrency_exceeded', retry_after_s: 1 }), {
        status: 429,
        headers: { 'Content-Type': 'application/json', 'Retry-After': '1' },
      });

    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/chat/')) return concurrency429();
      return jsonResponse({ messages: [], pendingApprovals: [] });
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useConversationFlow({ conversationId: 'cid-429' }), {
      wrapper: makeQCWrapper(),
    });
    await act(async () => {
      await Promise.resolve();
    });

    await act(async () => {
      await result.current.sendMessage('hello');
    });

    const assistant = result.current.messages.find((m) => m.role === 'assistant');
    expect(assistant).toBeDefined();
    // The typed code is stamped (drives chat.streamError localization), and the
    // generic connectionError fallback was NOT applied.
    expect(assistant!.errorCode).toBe('sse_concurrency_exceeded');
    expect(assistant!.content).not.toBe(CONNECTION_ERROR_COPY);
    expect(assistant!.status).toBe('done');

    // Retry-After surfaced to the user.
    expect(toastError).toHaveBeenCalledTimes(1);

    // Rendered through the real bubble: the specific localized message shows,
    // and the generic "Ошибка соединения" does not.
    render(<MessageBubble message={assistant!} />);
    expect(screen.getByText(CONCURRENCY_COPY)).toBeInTheDocument();
    expect(screen.queryByText(CONNECTION_ERROR_COPY)).not.toBeInTheDocument();
  });

  it('still falls back to connectionError on a genuine network throw', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/chat/')) throw new TypeError('Failed to fetch');
      return jsonResponse({ messages: [], pendingApprovals: [] });
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useConversationFlow({ conversationId: 'cid-net' }), {
      wrapper: makeQCWrapper(),
    });
    await act(async () => {
      await Promise.resolve();
    });

    await act(async () => {
      await result.current.sendMessage('hello');
    });

    const assistant = result.current.messages.find((m) => m.role === 'assistant');
    expect(assistant).toBeDefined();
    expect(assistant!.content).toBe(CONNECTION_ERROR_COPY);
    expect(assistant!.errorCode).toBeUndefined();
  });
});
