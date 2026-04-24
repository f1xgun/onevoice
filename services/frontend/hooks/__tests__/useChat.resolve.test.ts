import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { toast } from 'sonner';
import { useChat } from '../useChat';
import { useAuthStore } from '@/lib/auth';
import { mockSSEResponse, sseLine } from '@/test-utils/sse-mock';
import { singleCallBatch } from '@/test-utils/pending-approval-fixtures';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

/**
 * Sets up a `useChat` hook that is already in the `pendingApproval !== null`
 * state via hydration from GET /messages. The third fetch call is left for
 * the individual test to configure (the resolve response). After that, the
 * fourth (if reached) is the resume SSE stream.
 */
function hydratedHook(conversationId: string, fetchMock: ReturnType<typeof vi.fn>) {
  // GET /messages — returns the hydration envelope.
  fetchMock.mockImplementationOnce(async () => {
    return new Response(JSON.stringify({ messages: [], pendingApprovals: [singleCallBatch] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fetchMock);
  return renderHook(() => useChat(conversationId));
}

describe('useChat.resolveApproval — happy path', () => {
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

  it('POSTs resolve then opens resume SSE; pendingApproval clears on done; same assistant message', async () => {
    const fetchMock = vi.fn();
    // Hook setup first — hydrated with singleCallBatch (batchId "batch-single").
    fetchMock.mockImplementationOnce(async () => {
      return new Response(
        JSON.stringify({
          messages: [
            {
              id: 'assistant-existing',
              role: 'assistant',
              content: 'thinking… ',
              toolCalls: [],
              toolResults: [],
            },
          ],
          pendingApprovals: [singleCallBatch],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    });
    // 2nd: resolve 200 plain JSON
    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL, init: RequestInit) => {
      expect(String(input)).toMatch(
        /\/api\/v1\/conversations\/cid-resolve-1\/pending-tool-calls\/batch-single\/resolve$/
      );
      expect(init.method).toBe('POST');
      // CRITICAL: body must not echo tool_name anywhere.
      const body = init.body as string;
      expect(body).toBeDefined();
      expect(body).not.toContain('tool_name');
      const parsed = JSON.parse(body);
      expect(parsed).toEqual({
        decisions: [{ id: 'call-single-1', action: 'approve' }],
      });
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    // 3rd: resume SSE emits tool_result + done
    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL) => {
      expect(String(input)).toMatch(
        /\/api\/v1\/chat\/cid-resolve-1\/resume\?batch_id=batch-single$/
      );
      return mockSSEResponse([
        sseLine({
          type: 'tool_result',
          tool_call_id: 'srv-1',
          tool_name: 'telegram__send_channel_post',
          result: { message_id: 42 },
        }),
        sseLine({ type: 'done' }),
      ]);
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useChat('cid-resolve-1'));
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.pendingApproval).not.toBeNull();

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    expect(result.current.pendingApproval).toBeNull();
    expect(result.current.isStreaming).toBe(false);
    // Only one assistant message exists — resume did NOT push a new one.
    const assistants = result.current.messages.filter((m) => m.role === 'assistant');
    expect(assistants).toHaveLength(1);
    // And it got a tool_result applied (status done, result present).
    // applySSEEvent matches by tool_call_id; fallback by name+pending if not found.
    // In this hydration scenario there's no pre-existing pending toolCall, so the
    // tool_result does not mutate the message — the assertion above (single assistant, streaming=false) covers
    // the D-10 "same-message" invariant.
    expect(toast.error).not.toHaveBeenCalled();
  });
});

describe('useChat.resolveApproval — error branches', () => {
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

  it('409 → Russian "operation already processed" toast, card stays open, no resume fetch', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedHook('cid-409', fetchMock);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ error: 'batch resolving', retry_after_ms: 500 }), {
        status: 409,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    expect(toast.error).toHaveBeenCalledWith('Ошибка: операция уже была обработана');
    // Card stays open.
    expect(result.current.pendingApproval).not.toBeNull();
    // Total fetch calls so far: GET /messages + resolve (no resume).
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('403 with reason=policy_revoked → policy-revoked toast, card stays open', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedHook('cid-policy', fetchMock);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ reason: 'policy_revoked' }), {
        status: 403,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    expect(toast.error).toHaveBeenCalledWith('Отказано: инструмент запрещён текущей политикой');
    expect(result.current.pendingApproval).not.toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('500 → generic connection toast, card stays open', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedHook('cid-500', fetchMock);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ error: 'internal' }), {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    expect(toast.error).toHaveBeenCalledWith('Ошибка соединения — попробуйте ещё раз');
    expect(result.current.pendingApproval).not.toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('network-thrown on resolve → generic connection toast, card stays open', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedHook('cid-net', fetchMock);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    fetchMock.mockImplementationOnce(async () => {
      throw new TypeError('Failed to fetch');
    });

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    expect(toast.error).toHaveBeenCalledWith('Ошибка соединения — попробуйте ещё раз');
    expect(result.current.pendingApproval).not.toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('resume SSE error AFTER resolve 200 → RESUME toast, pendingApproval cleared, isStreaming false', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedHook('cid-resume-err', fetchMock);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // resolve OK
    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    // resume throws
    fetchMock.mockImplementationOnce(async () => {
      throw new TypeError('network gone');
    });

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    expect(toast.error).toHaveBeenCalledWith('Ошибка продолжения — перезагрузите страницу');
    expect(result.current.pendingApproval).toBeNull();
    expect(result.current.isStreaming).toBe(false);
  });
});

describe('useChat.resolveApproval — tool_name echo guard', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({
      user: null,
      accessToken: 'test-token',
      isAuthenticated: true,
    });
  });

  it('strips tool_name from edited_args before JSON.stringify', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedHook('cid-strip', fetchMock);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // resolve OK
    fetchMock.mockImplementationOnce(async (_input: RequestInfo | URL, init: RequestInit) => {
      const body = init.body as string;
      expect(body).not.toContain('"tool_name"');
      // Confirm the user edit survived.
      expect(body).toContain('"text":"edited"');
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    // resume — empty SSE, just closes.
    fetchMock.mockImplementationOnce(async () => mockSSEResponse([sseLine({ type: 'done' })]));

    await act(async () => {
      await result.current.resolveApproval([
        {
          id: 'call-single-1',
          action: 'edit',
          // Caller accidentally includes tool_name — we must strip it.
          edited_args: {
            text: 'edited',
            tool_name: 'forged_name',
          } as unknown as Record<string, string | number | boolean>,
        },
      ]);
    });

    expect(fetchMock).toHaveBeenCalledTimes(3);
  });
});
