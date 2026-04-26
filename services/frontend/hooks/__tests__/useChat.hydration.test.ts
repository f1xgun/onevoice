import { createElement, type ReactNode } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, renderHook, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useChat } from '../useChat';
import { useAuthStore } from '@/lib/auth';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { singleCallBatch, expiredBatch } from '@/test-utils/pending-approval-fixtures';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock the axios-based api client used by ChatWindow's `fetchConversation` and
// `useProjectsQuery`. Mirrors the pattern in ChatWindow.test.tsx so the
// ToolApprovalCard integration test below can mount the full component
// without hitting the network for non-HITL data.
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

// JSX-free wrapper so the file can stay as `.ts` (per Plan 17-08 done-criteria
// grep paths) while still mounting `<ChatWindow />` for the integration test.
function QueryWrapper({ children }: { children: ReactNode }) {
  return createElement(QueryClientProvider, { client: makeQueryClient() }, children);
}

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

  // ── Plan 17-08 GAP-03 belt-and-braces — frontend hydration consumer ─────
  //
  // These tests pin the contract that `useChat` correctly surfaces the
  // `pendingApprovals[0]` envelope from `GET /messages` to the consumer
  // (`ChatWindow` → `ToolApprovalCard`). The original GAP-03 was a backend
  // regression (empty identity fields on persisted batches → API returns
  // empty `pendingApprovals[]`); these tests ensure that even after that
  // backend fix lands, a future regression on the frontend half (e.g.
  // accidentally dropping the envelope path in `useChat`) fails loudly here
  // instead of silently breaking reload-recovery in production.

  it('hydrates pendingApproval state when GET /messages returns a non-empty pendingApprovals array', async () => {
    // Re-asserts the hook-level contract specifically labelled per Plan 17-08
    // §"Required gap-closure fix" item 4 (the existing test above asserts the
    // same shape; this one matches the spec's wording verbatim so its grep
    // anchor — `hydrates pendingApproval` — survives any future test reshuffle).
    const fetchMock = vi.fn().mockImplementationOnce(async () => {
      return new Response(JSON.stringify({ messages: [], pendingApprovals: [singleCallBatch] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useChat('cid-hydrate-gap03'));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.pendingApproval).not.toBeNull();
    expect(result.current.pendingApproval!.batchId).toBe(singleCallBatch.batchId);
    expect(result.current.pendingApproval!.calls).toHaveLength(1);
  });

  it('renders ToolApprovalCard via ChatWindow when hydration succeeds (integration regression net)', async () => {
    // Belt-and-braces: prove the hook → ChatWindow → ToolApprovalCard wire is
    // intact end-to-end. If a future change drops the `pendingApprovals[0]`
    // path in `useChat` OR ChatWindow stops passing `pendingApproval` to the
    // card, this test fails BEFORE production reload-recovery breaks again.
    const fetchMock = vi.fn().mockImplementation(async () => {
      // ChatWindow's `useChat` performs ONE fetch on mount (GET /messages).
      // No other fetches fire until the user submits a decision, so a single
      // implementation suffices for this render.
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
    // Subtitle from UI-SPEC §Copywriting Contract — confirms the card body
    // mounted, not just the outer region.
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

    // The ChatWindow empty-state text appears once `isLoading === false` and
    // `messages.length === 0`. Wait for it before asserting the absence of
    // the card so this is not a false-negative race.
    await screen.findByText('Чем могу помочь?');
    expect(screen.queryByRole('region', { name: /Ожидает подтверждения/ })).not.toBeInTheDocument();
  });

  // Plan 17-08 Test 4 (negative — expired): the plan specifies that if
  // `useChat` does NOT filter expired batches, the test should be marked
  // `it.skip` with a comment. The current `useChat.normalizePendingApproval`
  // intentionally preserves `status === 'expired'` so the UI layer (Plan
  // 17-05 `ExpiredApprovalBanner`) owns the render decision (CONTEXT.md
  // D-11). Keeping this skipped so a future contract flip — moving the
  // expired filter into the hook — has a place to land without re-deriving
  // the test surface from scratch. The existing "STILL sets pendingApproval"
  // assertion above covers the current behaviour positively.
  it.skip('does not hydrate pendingApproval when the batch is expired (TODO: gated on useChat filter)', () => {
    // Intentionally skipped — see preceding block comment.
  });
});
