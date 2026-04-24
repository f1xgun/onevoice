import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useChat } from '../useChat';
import { useAuthStore } from '@/lib/auth';
import { mockSSEResponse, sseLine } from '@/test-utils/sse-mock';

// Mock sonner so we can assert on toast-free pending arrival.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

describe('useChat — SSE tool_approval_required arrival', () => {
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
    // 1) GET /messages — empty history, no pending.
    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL) => {
      expect(String(input)).toMatch(/\/api\/v1\/conversations\/.+\/messages$/);
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    // 2) POST /chat/{id} — SSE stream with a partial text then tool_approval_required, then natural close.
    fetchMock.mockImplementationOnce(async (input: RequestInfo | URL) => {
      expect(String(input)).toMatch(/\/api\/v1\/chat\/cid-1$/);
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

    const { result } = renderHook(() => useChat('cid-1'));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      await result.current.sendMessage('post hi');
    });

    // Stream closed naturally → not streaming anymore.
    expect(result.current.isStreaming).toBe(false);
    // Pending approval has been hydrated from the SSE event.
    expect(result.current.pendingApproval).not.toBeNull();
    expect(result.current.pendingApproval!.batchId).toBe('b1');
    expect(result.current.pendingApproval!.calls).toHaveLength(1);
    expect(result.current.pendingApproval!.calls[0].callId).toBe('c1');
    expect(result.current.pendingApproval!.calls[0].toolName).toBe('telegram__send_channel_post');
    expect(result.current.pendingApproval!.calls[0].editableFields).toEqual(['text']);
    expect(result.current.pendingApproval!.calls[0].floor).toBe('manual');
    // createdAt is synthesized on SSE arrival — just assert it's a non-empty ISO-ish string.
    expect(typeof result.current.pendingApproval!.createdAt).toBe('string');
    expect(result.current.pendingApproval!.createdAt.length).toBeGreaterThan(0);
    expect(result.current.pendingApproval!.status).toBe('pending');

    // Partial content before the approval event is preserved on the assistant message.
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

    const { result } = renderHook(() => useChat('cid-2'));
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await act(async () => {
      await result.current.sendMessage('anything');
    });

    // abort() must not be invoked on the tool_approval_required path (Pitfall 2).
    expect(abortSpy).not.toHaveBeenCalled();
    expect(result.current.pendingApproval).not.toBeNull();
  });
});
