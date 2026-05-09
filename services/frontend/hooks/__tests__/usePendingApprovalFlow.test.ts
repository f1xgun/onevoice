// Tests for `usePendingApprovalFlow` — the sibling hook split out of
// `useChat` in Phase 19 plan 19-10 (decision D-19). Per D-16, assertions
// from the pre-split `useChat.hydration.test.ts` and `useChat.resolve.test.ts`
// are reproduced byte-identically; only the test setup changes (renderHook
// targets the new hook signature instead of the old positional `useChat(id)`).
//
// Tests use two patterns:
//
// (A) Pure approval-flow tests: render `usePendingApprovalFlow` directly
//     with `onResumeEvent: noop`. Used when the assertion only inspects
//     pendingApproval / resolveApproval / toast / fetch.
//
// (B) Combined-wiring tests: render the canonical ChatWindow wiring of
//     useChat + usePendingApprovalFlow via the `useChatWithApprovalFlow`
//     helper. Used when the assertion also inspects `messages` /
//     `isStreaming` (e.g., the resume-stream happy path).
//
// (C) ChatWindow integration tests: render `<ChatWindow>` and inspect the
//     real component tree.

import { createElement, useEffect, useRef, type ReactNode } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, render, screen, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { toast } from 'sonner';
import { useChat } from '../useChat';
import { usePendingApprovalFlow } from '../usePendingApprovalFlow';
import { useAuthStore } from '@/lib/auth';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { mockSSEResponse, sseLine } from '@/test-utils/sse-mock';
import { singleCallBatch, expiredBatch } from '@/test-utils/pending-approval-fixtures';
import type { PendingApproval } from '@/types/chat';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock the axios-based api client used by ChatWindow's `fetchConversation`,
// `useProjectsQuery`, and `useMoveConversation`. Mirrors the existing
// ChatWindow.test.tsx idiom so render-based integration tests below can mount
// the full component without hitting the network for non-HITL data.
vi.mock('@/lib/api', () => ({
  api: {
    get: (url: string) => {
      if (url.startsWith('/conversations/')) {
        return Promise.resolve({
          data: { id: 'conv-1', title: 'Test Conversation', projectId: null },
        });
      }
      if (url === '/projects') {
        return Promise.resolve({ data: [] });
      }
      return Promise.resolve({ data: null });
    },
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  });
}

function QueryWrapper({ children }: { children: ReactNode }) {
  return createElement(QueryClientProvider, { client: makeQueryClient() }, children);
}

function makeQCWrapper() {
  const qc = makeQueryClient();
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

// Pattern (A): combined-wiring setup that arrives at the
// pendingApproval-hydrated state via GET /messages. Mirrors how ChatWindow
// wires the two sibling hooks; the first fetch is the envelope, additional
// fetches (resolve / resume) are configured per-test through fetchMock.
function hydratedFlow(conversationId: string, fetchMock: ReturnType<typeof vi.fn>) {
  fetchMock.mockImplementationOnce(async () => {
    return new Response(JSON.stringify({ messages: [], pendingApprovals: [singleCallBatch] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fetchMock);
  return renderHook(() => useChatWithApprovalFlow(conversationId), {
    wrapper: makeQCWrapper(),
  });
}

// Pattern (B): combined ChatWindow wiring. Used for tests that also inspect
// `messages` or `isStreaming` from useChat.
function useChatWithApprovalFlow(conversationId: string) {
  const approvalFlowRef = useRef<{ setPending: (a: PendingApproval) => void } | null>(null);
  const chat = useChat({
    conversationId,
    onApprovalRequired: (approval) => approvalFlowRef.current?.setPending(approval),
  });
  const approvalFlow = usePendingApprovalFlow({
    conversationId,
    onResumeEvent: chat.appendSSEEvent,
  });
  useEffect(() => {
    approvalFlowRef.current = approvalFlow;
  });
  return {
    ...chat,
    pendingApproval: approvalFlow.pendingApproval,
    resolveApproval: approvalFlow.resolveApproval,
    isResolving: approvalFlow.isResolving,
  };
}

// ─────────────────────────────────────────────────────────────────────────────
// Hydration tests — moved verbatim from hooks/__tests__/useChat.hydration.test.ts
// (D-16 wiring-only updates: `useChat(string)` → `usePendingApprovalFlow({…})`).
// ─────────────────────────────────────────────────────────────────────────────

describe('usePendingApprovalFlow — hydration from GET /messages pendingApprovals', () => {
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

  // Note: After the Phase 19 hook split, `useChat` is the sole fetcher of
  // GET /messages (preserving the single round-trip invariant). Hydration
  // tests therefore exercise the combined `useChatWithApprovalFlow` wiring;
  // assertions on `result.current.pendingApproval` remain byte-identical
  // (D-16: wiring-only test changes).

  it('hydrates pendingApproval from a non-empty pendingApprovals array on mount', async () => {
    const fetchMock = vi.fn();
    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL) => {
      expect(String(input)).toMatch(/\/messages$/);
      return new Response(
        JSON.stringify({
          messages: [],
          pendingApprovals: [singleCallBatch],
        }),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useChatWithApprovalFlow('cid-hydrate-1'), {
      wrapper: QueryWrapper,
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.pendingApproval).not.toBeNull();
    // Fixture shape is already camelCase; deep-equal should pass.
    expect(result.current.pendingApproval).toEqual(singleCallBatch);
  });

  it('leaves pendingApproval null when pendingApprovals is empty', async () => {
    const fetchMock = vi.fn();
    fetchMock.mockImplementationOnce(async () => {
      return new Response(
        JSON.stringify({
          messages: [],
          pendingApprovals: [],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useChatWithApprovalFlow('cid-hydrate-empty'), {
      wrapper: QueryWrapper,
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.pendingApproval).toBeNull();
  });

  it('leaves pendingApproval null when GET /messages returns legacy ApiMessage[] (no envelope)', async () => {
    const fetchMock = vi.fn();
    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify([]), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useChatWithApprovalFlow('cid-hydrate-legacy'), {
      wrapper: QueryWrapper,
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.pendingApproval).toBeNull();
  });

  it('STILL sets pendingApproval when the first batch is expired (UI-layer decides rendering)', async () => {
    const fetchMock = vi.fn();
    fetchMock.mockImplementationOnce(async () => {
      return new Response(
        JSON.stringify({
          messages: [],
          pendingApprovals: [expiredBatch],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useChatWithApprovalFlow('cid-hydrate-expired'), {
      wrapper: QueryWrapper,
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.pendingApproval).not.toBeNull();
    expect(result.current.pendingApproval!.status).toBe('expired');
    expect(result.current.pendingApproval!.batchId).toBe('batch-expired');
  });

  // ── Belt-and-braces — frontend hydration consumer ─────
  it('hydrates pendingApproval state when GET /messages returns a non-empty pendingApprovals array', async () => {
    const fetchMock = vi.fn().mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [singleCallBatch] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useChatWithApprovalFlow('cid-hydrate-gap03'), {
      wrapper: QueryWrapper,
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.pendingApproval).not.toBeNull();
    expect(result.current.pendingApproval!.batchId).toBe(singleCallBatch.batchId);
    expect(result.current.pendingApproval!.calls).toHaveLength(1);
  });

  it('renders ToolApprovalCard via ChatWindow when hydration succeeds (integration regression net)', async () => {
    const fetchMock = vi.fn().mockImplementation(async () => {
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [singleCallBatch] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    render(
      createElement(QueryWrapper, null, createElement(ChatWindow, { conversationId: 'conv-1' }))
    );

    const region = await screen.findByRole('region', { name: /Ожидает подтверждения/ });
    expect(region).toBeInTheDocument();
    expect(screen.getByText('Проверьте аргументы перед выполнением')).toBeInTheDocument();
  });

  it('does not render ToolApprovalCard via ChatWindow when pendingApprovals is empty (negative)', async () => {
    const fetchMock = vi.fn().mockImplementation(async () => {
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    render(
      createElement(QueryWrapper, null, createElement(ChatWindow, { conversationId: 'conv-1' }))
    );

    await screen.findByText('Чем могу помочь?');
    expect(screen.queryByRole('region', { name: /Ожидает подтверждения/ })).not.toBeInTheDocument();
  });

  it.skip('does not hydrate pendingApproval when the batch is expired (TODO: gated on filter)', () => {
    // Intentionally skipped — see preceding block comment.
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Resolve tests — moved verbatim from hooks/__tests__/useChat.resolve.test.ts
// (D-16 wiring-only updates).
// ─────────────────────────────────────────────────────────────────────────────

describe('usePendingApprovalFlow.resolveApproval — happy path', () => {
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
    // 1st: GET /messages — hydrated with singleCallBatch + one assistant message.
    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith('/messages')) {
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
      }
      if (url.endsWith('/resolve')) {
        expect(url).toMatch(
          /\/api\/v1\/conversations\/cid-resolve-1\/pending-tool-calls\/batch-single\/resolve$/
        );
        expect(init!.method).toBe('POST');
        // CRITICAL: body must not echo tool_name anywhere.
        const body = init!.body as string;
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
      }
      // Resume SSE.
      expect(url).toMatch(/\/api\/v1\/chat\/cid-resolve-1\/resume\?batch_id=batch-single$/);
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

    // Pattern (B): combined wiring — assertion below inspects messages.
    const { result } = renderHook(() => useChatWithApprovalFlow('cid-resolve-1'), {
      wrapper: makeQCWrapper(),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

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
    // the "same-message" invariant.
    expect(toast.error).not.toHaveBeenCalled();
  });
});

describe('usePendingApprovalFlow.resolveApproval — error branches', () => {
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
    const { result } = hydratedFlow('cid-409', fetchMock);
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

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

  it('403 with reason=policy_revoked → 403 business-scope toast wins (precedence), card stays open', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedFlow('cid-policy', fetchMock);
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ reason: 'policy_revoked' }), {
        status: 403,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    expect(toast.error).toHaveBeenCalledWith('Отказано: операция вне вашей бизнес-области');
    expect(result.current.pendingApproval).not.toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('400 with reason=policy_revoked → policy-revoked toast (precedence preserved on non-403), card stays open', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedFlow('cid-policy-400', fetchMock);
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ reason: 'policy_revoked' }), {
        status: 400,
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
    const { result } = hydratedFlow('cid-500', fetchMock);
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

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
    const { result } = hydratedFlow('cid-net', fetchMock);
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

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
    // Pattern (B): isStreaming assertion → use combined wiring.
    const fetchMock = vi.fn();
    fetchMock.mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith('/messages')) {
        return new Response(JSON.stringify({ messages: [], pendingApprovals: [singleCallBatch] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      if (url.endsWith('/resolve')) {
        return new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      // Resume → throws.
      throw new TypeError('network gone');
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useChatWithApprovalFlow('cid-resume-err'), {
      wrapper: makeQCWrapper(),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    expect(toast.error).toHaveBeenCalledWith('Ошибка продолжения — перезагрузите страницу');
    expect(result.current.pendingApproval).toBeNull();
    expect(result.current.isStreaming).toBe(false);
  });
});

describe('usePendingApprovalFlow.resolveApproval — tool_name echo guard', () => {
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
    const { result } = hydratedFlow('cid-strip', fetchMock);
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

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

// ─────────────────────────────────────────────────────────────────────────────
// New approval-flow-specific tests (Phase 19, plan 19-10 acceptance criteria).
// ─────────────────────────────────────────────────────────────────────────────

describe('usePendingApprovalFlow — sanitization & contracts', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({
      user: null,
      accessToken: 'test-token',
      isAuthenticated: true,
    });
  });

  it('clamps reject_reason to 500 chars before POST', async () => {
    const fetchMock = vi.fn();
    const { result } = hydratedFlow('cid-clamp', fetchMock);
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

    const longReason = 'x'.repeat(800);
    fetchMock.mockImplementationOnce(async (_input: RequestInfo | URL, init: RequestInit) => {
      const body = init.body as string;
      const parsed = JSON.parse(body) as {
        decisions: Array<{ reject_reason?: string }>;
      };
      expect(parsed.decisions[0].reject_reason).toBeDefined();
      expect(parsed.decisions[0].reject_reason!.length).toBe(500);
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    fetchMock.mockImplementationOnce(async () => mockSSEResponse([sseLine({ type: 'done' })]));

    await act(async () => {
      await result.current.resolveApproval([
        { id: 'call-single-1', action: 'reject', reject_reason: longReason },
      ]);
    });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('forwards each resume SSE frame through onResumeEvent', async () => {
    // Seed via setPending directly so we can observe the unique
    // onResumeEvent callback (no chat.appendSSEEvent indirection).
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const onResume = vi.fn();
    const { result } = renderHook(
      () => usePendingApprovalFlow({ conversationId: 'cid-forward', onResumeEvent: onResume }),
      { wrapper: makeQCWrapper() }
    );
    await act(async () => {
      result.current.setPending(singleCallBatch);
    });
    expect(result.current.pendingApproval).not.toBeNull();

    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    fetchMock.mockImplementationOnce(async () =>
      mockSSEResponse([
        sseLine({
          type: 'tool_result',
          tool_call_id: 'r1',
          tool_name: 'telegram__send_channel_post',
          result: { message_id: 7 },
        }),
        sseLine({ type: 'done' }),
      ])
    );

    await act(async () => {
      await result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
    });

    // Forwarded both frames in order.
    expect(onResume).toHaveBeenCalledTimes(2);
    expect((onResume.mock.calls[0][0] as Record<string, unknown>).type).toBe('tool_result');
    expect((onResume.mock.calls[1][0] as Record<string, unknown>).type).toBe('done');
  });

  it('resolveApproval is a no-op while a previous resolve is in flight (debounce)', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(
      () =>
        usePendingApprovalFlow({
          conversationId: 'cid-debounce',
          onResumeEvent: () => undefined,
        }),
      { wrapper: makeQCWrapper() }
    );
    await act(async () => {
      result.current.setPending(singleCallBatch);
    });
    expect(result.current.pendingApproval).not.toBeNull();

    // Hold the resolve response until we manually settle it. While in flight,
    // a second call to resolveApproval must be a no-op (no extra fetch).
    let releaseResolve: (() => void) | null = null;
    const resolvePromise = new Promise<Response>((resolve) => {
      releaseResolve = () => {
        resolve(
          new Response(JSON.stringify({ ok: true }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      };
    });
    fetchMock.mockImplementationOnce(() => resolvePromise);
    fetchMock.mockImplementationOnce(async () => mockSSEResponse([sseLine({ type: 'done' })]));

    await act(async () => {
      const first = result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
      // Second call while first is in flight — should be debounced.
      const second = result.current.resolveApproval([{ id: 'call-single-1', action: 'approve' }]);
      releaseResolve!();
      await Promise.all([first, second]);
    });

    // Exactly ONE resolve + ONE resume. The second resolveApproval call did
    // NOT fire a duplicate POST.
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
