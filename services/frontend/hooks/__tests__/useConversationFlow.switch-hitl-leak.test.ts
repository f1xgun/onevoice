// Regression: switching to a DIFFERENT conversation must not leak the prior
// conversation's HITL approval state into the new one.
//
// Navigation between conversations is router.push('/chat/${id}') / <Link> on the
// SAME /chat/[id] dynamic route, so the App Router REUSES the ChatWindow /
// useConversationFlow instance (no remount, the unmount-abort effect never
// fires). Prior fixes guarded only the SEND stream. Two HITL leaks remained when
// switching away from a conversation that had an active approval:
//
// (1) pendingApproval was only cleared in resolveApproval's finally, never on a
//     conversationId switch — so conversation A's ToolApprovalCard stayed
//     rendered over B (and B's composer is disabled while pendingApproval !==
//     null; submitting the leaked card POSTs to the wrong batch → 403).
//
// (2) the HITL resume SSE stream used resumeAbortRef, aborted ONLY on unmount —
//     so A's resume frames kept flowing after the switch and bled into B (a
//     `done` frame finalizing B's bubble, a `tool_approval_required` re-pause
//     frame injecting A's next batch as a card in B).
//
// The fix detects a REAL switch in the load effect and on it clears
// pendingApproval, resets resolving state, and aborts the resume stream;
// resumingConversationIdRef scopes the resume's onEvent/finally writes to their
// own conversation.
//
// Fail-on-revert: drop the `setPendingApproval(null)` from the switch detection
// and test (1) fails (A's card lingers over B).

import { type ReactNode, createElement } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useConversationFlow } from '../useConversationFlow';
import { useAuthStore } from '@/lib/auth';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// Fixed active business — these scenarios vary conversationId, not the business.
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

const batchA = {
  batchId: 'batch-a',
  status: 'pending',
  createdAt: new Date().toISOString(),
  calls: [
    {
      callId: 'call-a-1',
      toolName: 'telegram__send_channel_post',
      args: { text: 'A announcement' },
      editableFields: [],
      floor: 'manual',
    },
  ],
};

const batchB = {
  batchId: 'batch-b',
  status: 'pending',
  createdAt: new Date().toISOString(),
  calls: [
    {
      callId: 'call-b-1',
      toolName: 'vk__publish_post',
      args: { text: 'B announcement' },
      editableFields: [],
      floor: 'manual',
    },
  ],
};

// A controllable SSE response: stays open until close() is called, keeping the
// resume stream in flight across the conversation switch so the abort guard is
// exercised mid-resume deterministically.
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

describe('useConversationFlow — switching conversations does not leak HITL state', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({ user: null, accessToken: 'test-token', isAuthenticated: true });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('clears a hydrated pendingApproval from A when conversationId flips to B with no approvals', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      // A hydrates with an active approval card; B has none.
      return jsonResponse(
        url.includes('cid-B')
          ? { messages: [], pendingApprovals: [] }
          : { messages: [], pendingApprovals: [batchA] }
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result, rerender } = renderHook(
      ({ conversationId }: { conversationId: string }) => useConversationFlow({ conversationId }),
      { wrapper: makeQCWrapper(), initialProps: { conversationId: 'cid-A' } }
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    // A's approval card is hydrated and rendered.
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());
    expect(result.current.pendingApproval!.batchId).toBe('batch-a');

    // Switch to conversation B (same dynamic route → no remount).
    await act(async () => {
      rerender({ conversationId: 'cid-B' });
      await new Promise((r) => setTimeout(r, 30));
    });

    // The leaked card must be gone — B has no pending approval.
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.pendingApproval).toBeNull();
  });

  it('aborts the in-flight resume stream for A and loads B unpolluted on a mid-resume switch', async () => {
    const gate = gatedSSEResponse();
    let resumeAborted = false;

    const convBHistory = {
      messages: [
        { id: 'b-u1', role: 'user', content: 'B prompt', toolCalls: [] },
        { id: 'b-a1', role: 'assistant', content: 'B reply', toolCalls: [] },
      ],
      pendingApprovals: [],
    };

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith('/resolve')) {
        return jsonResponse({ ok: true });
      }
      if (url.includes('/resume')) {
        init?.signal?.addEventListener('abort', () => {
          resumeAborted = true;
        });
        return gate.response;
      }
      // History GETs.
      if (url.includes('cid-B')) return jsonResponse(convBHistory);
      return jsonResponse({ messages: [], pendingApprovals: [batchA] });
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result, rerender } = renderHook(
      ({ conversationId }: { conversationId: string }) => useConversationFlow({ conversationId }),
      { wrapper: makeQCWrapper(), initialProps: { conversationId: 'cid-A' } }
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

    // Approve A's batch — opens the gated resume SSE and keeps it open.
    let resolvePromise!: Promise<void>;
    await act(async () => {
      resolvePromise = result.current.resolveApproval([{ id: 'call-a-1', action: 'approve' }]);
      await new Promise((r) => setTimeout(r, 10));
    });
    expect(result.current.isResolving).toBe(true);

    // Switch to conversation B mid-resume (same dynamic route → no remount).
    await act(async () => {
      rerender({ conversationId: 'cid-B' });
      await new Promise((r) => setTimeout(r, 30));
    });

    // A's resume stream was aborted by the switch.
    expect(resumeAborted).toBe(true);

    // B's history loaded clean — no A approval card, no A bubble.
    await waitFor(() => {
      expect(result.current.messages.some((m) => m.id === 'b-a1')).toBe(true);
    });
    expect(result.current.pendingApproval).toBeNull();
    expect(result.current.messages.some((m) => m.content === 'B reply')).toBe(true);

    // Now push A's orphaned resume frames AFTER the switch: a `done` (would
    // finalize B's bubble) and a re-pause (would inject A's next batch as a card
    // in B). The guard must drop both — B stays unpolluted.
    await act(async () => {
      gate.push({ type: 'done' });
      gate.push({
        type: 'tool_approval_required',
        batch_id: 'batch-a-next',
        calls: [
          {
            call_id: 'call-a-next-1',
            tool_name: 'vk__publish_post',
            args: {},
            editable_fields: [],
            floor: 'manual',
          },
        ],
      });
      await new Promise((r) => setTimeout(r, 10));
    });

    // No A approval bled into B.
    expect(result.current.pendingApproval).toBeNull();
    expect(result.current.messages.some((m) => m.content === 'B reply')).toBe(true);
    expect(result.current.messages.some((m) => m.id === 'b-a1')).toBe(true);

    // Settle the orphaned resume (already aborted) without leaking.
    await act(async () => {
      gate.close();
      await resolvePromise;
    });
  });

  // Fail-on-revert target for the resume `finally` ownership guard. B has its
  // OWN pending approval. A's orphaned resume settles (its `finally` runs)
  // AFTER the switch to B; without the guard A's `finally` calls
  // setPendingApproval(null) and wipes B's freshly-hydrated batch-b card.
  it("preserves B's own pending approval when A's orphaned resume settles after the switch", async () => {
    const gate = gatedSSEResponse();

    const convBHistory = {
      messages: [
        { id: 'b-u1', role: 'user', content: 'B prompt', toolCalls: [] },
        { id: 'b-a1', role: 'assistant', content: 'B reply', toolCalls: [] },
      ],
      pendingApprovals: [batchB],
    };

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith('/resolve')) {
        return jsonResponse({ ok: true });
      }
      if (url.includes('/resume')) {
        void init?.signal;
        return gate.response;
      }
      // History GETs: B hydrates its own batch-b approval.
      if (url.includes('cid-B')) return jsonResponse(convBHistory);
      return jsonResponse({ messages: [], pendingApprovals: [batchA] });
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result, rerender } = renderHook(
      ({ conversationId }: { conversationId: string }) => useConversationFlow({ conversationId }),
      { wrapper: makeQCWrapper(), initialProps: { conversationId: 'cid-A' } }
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());
    expect(result.current.pendingApproval!.batchId).toBe('batch-a');

    // Approve A's batch — opens the gated resume SSE and keeps it open.
    let resolvePromise!: Promise<void>;
    await act(async () => {
      resolvePromise = result.current.resolveApproval([{ id: 'call-a-1', action: 'approve' }]);
      await new Promise((r) => setTimeout(r, 10));
    });
    expect(result.current.isResolving).toBe(true);

    // Switch to conversation B mid-resume (same dynamic route → no remount).
    // B hydrates its own batch-b approval card.
    await act(async () => {
      rerender({ conversationId: 'cid-B' });
      await new Promise((r) => setTimeout(r, 30));
    });

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() => expect(result.current.pendingApproval?.batchId).toBe('batch-b'));

    // Settle A's orphaned resume AFTER the switch: closing the gate wakes the
    // aborted reader, the resume loop exits, and resolveApproval's `finally`
    // runs in A's stale closure. The ownership guard must skip its
    // setPendingApproval(null) so B's batch-b card survives.
    await act(async () => {
      gate.close();
      await resolvePromise;
      await new Promise((r) => setTimeout(r, 10));
    });

    expect(result.current.pendingApproval).not.toBeNull();
    expect(result.current.pendingApproval!.batchId).toBe('batch-b');
  });
});
