import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import React from 'react';
import { useConversationFlow } from '../useConversationFlow';
import { useAuthStore } from '@/lib/auth';
import { mockSSEResponse, sseLine } from '@/test-utils/sse-mock';

// useConversationFlow (replaces the previous useChat + usePendingApprovalFlow
// split). The pendingApproval state, resolveApproval action,
// and live chat stream all live in one hook now, so these tests target the
// single hook directly instead of the previous useChatWithApprovalFlow
// wrapper that bridged the two siblings.

// Mock sonner so we can assert on toast-free pending arrival.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-test' }),
}));

// useConversationFlow consumes useQueryClient so it can invalidate
// ['conversations'] on chat SSE 'done'. All renderHook calls wrap a
// QueryClientProvider — the React Query cache is unused by these test
// scenarios (they cover SSE pause / resume / hydration paths) but the hook
// requires the context.
function makeQCWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
}

describe('useConversationFlow — SSE tool_approval_required arrival', () => {
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

  it('sets pendingApproval when tool_approval_required event arrives and stream closes naturally', async () => {
    const fetchMock = vi.fn();
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
      expect(String(input)).toMatch(/\/api\/v1\/businesses\/biz-test\/chat\/cid-1$/);
      return mockSSEResponse([
        sseLine({ type: 'text', content: 'I will post to ' }),
        sseLine({
          type: 'tool_approval_required',
          batch_id: 'b1',
          calls: [
            {
              call_id: 'c1',
              tool_name: 'telegram__send_channel_post',
              args: { chat_id: 1, text: 'hi' },
              editable_fields: ['text'],
              floor: 'manual',
            },
          ],
        }),
      ]);
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useConversationFlow({ conversationId: 'cid-1' }), {
      wrapper: makeQCWrapper(),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      await result.current.sendMessage('post hi');
    });

    expect(result.current.isStreaming).toBe(false);
    expect(result.current.pendingApproval).not.toBeNull();
    expect(result.current.pendingApproval!.batchId).toBe('b1');
    expect(result.current.pendingApproval!.calls).toHaveLength(1);
    expect(result.current.pendingApproval!.calls[0].callId).toBe('c1');
    expect(result.current.pendingApproval!.calls[0].toolName).toBe('telegram__send_channel_post');
    expect(result.current.pendingApproval!.calls[0].editableFields).toEqual(['text']);
    expect(result.current.pendingApproval!.calls[0].floor).toBe('manual');
    expect(typeof result.current.pendingApproval!.createdAt).toBe('string');
    expect(result.current.pendingApproval!.createdAt.length).toBeGreaterThan(0);
    expect(result.current.pendingApproval!.status).toBe('pending');

    const assistant = result.current.messages.find((m) => m.role === 'assistant');
    expect(assistant).toBeDefined();
    expect(assistant!.content).toBe('I will post to ');
  });

  it('does NOT abort the fetch controller on tool_approval_required — lets stream end naturally', async () => {
    const abortSpy = vi.spyOn(AbortController.prototype, 'abort');
    const fetchMock = vi.fn();
    fetchMock.mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    fetchMock.mockImplementationOnce(async () => {
      return mockSSEResponse([
        sseLine({
          type: 'tool_approval_required',
          batch_id: 'b2',
          calls: [
            {
              call_id: 'c1',
              tool_name: 'telegram__send_channel_post',
              args: { text: 'x' },
              editable_fields: [],
              floor: 'manual',
            },
          ],
        }),
      ]);
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useConversationFlow({ conversationId: 'cid-2' }), {
      wrapper: makeQCWrapper(),
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await act(async () => {
      await result.current.sendMessage('anything');
    });

    expect(abortSpy).not.toHaveBeenCalled();
    expect(result.current.pendingApproval).not.toBeNull();
  });
});
