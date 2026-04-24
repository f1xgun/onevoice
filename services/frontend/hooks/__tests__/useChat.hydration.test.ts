import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useChat } from '../useChat';
import { useAuthStore } from '@/lib/auth';
import { singleCallBatch, expiredBatch } from '@/test-utils/pending-approval-fixtures';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

describe('useChat — hydration from GET /messages pendingApprovals', () => {
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

    const { result } = renderHook(() => useChat('cid-hydrate-1'));
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

    const { result } = renderHook(() => useChat('cid-hydrate-empty'));
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

    const { result } = renderHook(() => useChat('cid-hydrate-legacy'));
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

    const { result } = renderHook(() => useChat('cid-hydrate-expired'));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.pendingApproval).not.toBeNull();
    expect(result.current.pendingApproval!.status).toBe('expired');
    expect(result.current.pendingApproval!.batchId).toBe('batch-expired');
  });
});
