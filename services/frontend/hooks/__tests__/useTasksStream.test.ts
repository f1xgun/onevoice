import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useTasksStream } from '../useTasksStream';
import { useAuthStore } from '@/lib/auth';

// Pins that the always-on tasks SSE stream goes through authFetch (so an
// access token that expired mid-stream is refreshed + replayed by authFetch,
// instead of the old tight 401 reconnect loop against a dead token), and
// that it parses + dispatches SSE events. The 401→refresh mechanics
// themselves are covered by lib/api/__tests__/authFetch.test.ts.

const authFetchMock = vi.hoisted(() => vi.fn());

vi.mock('@/lib/api/authFetch', () => ({ authFetch: authFetchMock }));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-test' }),
}));

// Minimal env-independent SSE Response: a body whose getReader() yields one
// chunk per event then done.
function sseStreamResponse(events: object[]): Response {
  const enc = new TextEncoder();
  const chunks = events.map((e) => enc.encode(`data: ${JSON.stringify(e)}\n\n`));
  let i = 0;
  const reader = {
    read: async () =>
      i < chunks.length
        ? { done: false, value: chunks[i++] }
        : { done: true, value: undefined as Uint8Array | undefined },
    cancel: async () => {},
    releaseLock: () => {},
  };
  return { ok: true, status: 200, body: { getReader: () => reader } } as unknown as Response;
}

describe('useTasksStream', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({ user: null, accessToken: 'test-token', isAuthenticated: true });
  });

  afterEach(() => {
    useAuthStore.setState({ user: null, accessToken: null, isAuthenticated: false });
  });

  it('opens the stream via authFetch and dispatches parsed events', async () => {
    const ev = { type: 'task.updated', task: { id: 't1' } };
    authFetchMock.mockResolvedValueOnce(sseStreamResponse([ev]));
    // Any reconnect attempt hangs so the test never loops.
    authFetchMock.mockReturnValue(new Promise<Response>(() => {}));

    const onEvent = vi.fn();
    const { unmount } = renderHook(() => useTasksStream(onEvent));

    await waitFor(() => expect(onEvent).toHaveBeenCalledWith(ev));

    expect(authFetchMock).toHaveBeenCalled();
    expect(authFetchMock.mock.calls[0][0]).toBe('/api/v1/businesses/biz-test/tasks/stream');
    // Goes through authFetch — never a raw fetch with a hand-written bearer.
    const init = authFetchMock.mock.calls[0][1] as RequestInit;
    expect(init.signal).toBeDefined();

    unmount();
  });
});
