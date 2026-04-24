import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { ChatWindow } from '../ChatWindow';
import { useAuthStore } from '@/lib/auth';
import { singleCallBatch, expiredBatch } from '@/test-utils/pending-approval-fixtures';

// Mock sonner so toast.error from unrelated flows is inert.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock the axios-based api client used by fetchConversation, useProjectsQuery,
// and useMoveConversation. Keep the shape minimal — tests only need GET to
// return the conversation envelope and a stub projects list.
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

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function mockGetMessages(responseBody: unknown) {
  const fetchMock = vi.fn().mockImplementation(async () => {
    return new Response(JSON.stringify(responseBody), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('ChatWindow — Phase 17 HITL integration (Invariants 5 + 9)', () => {
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

  it('Invariant 5: card hydrates from GET /messages.pendingApprovals on mount', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [singleCallBatch] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    const region = await screen.findByRole('region', { name: /Ожидает подтверждения/ });
    expect(region).toBeInTheDocument();
    // Subtitle matches UI-SPEC exactly.
    expect(screen.getByText('Проверьте аргументы перед выполнением')).toBeInTheDocument();
  });

  it('Invariant 9: composer input + Send button are HTML-disabled while pendingApproval is non-null', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [singleCallBatch] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    // Wait for card to render (proves hydration completed).
    await screen.findByRole('region', { name: /Ожидает подтверждения/ });

    // Composer input carries `disabled` attribute (HTML-level).
    const input = screen.getByPlaceholderText('Напишите сообщение...');
    expect(input).toBeDisabled();

    // Send button: the only button outside the approval card with an SVG
    // icon child (Send lucide icon). We find it by locating the composer
    // region and grabbing its button descendant. Simpler: find all buttons
    // and filter to the one that is the composer's Send button.
    //
    // Strategy: the Send button sits next to the input (same flex row) and
    // is either disabled + has svg-only content. We can query by its
    // disabled state: all other buttons on the page (toggle group, Submit)
    // are inside the card region; the composer Send lives outside.
    // @testing-library idiom: pick it via the shared parent <div>.
    const composerDiv = input.closest('div');
    expect(composerDiv).not.toBeNull();
    const sendBtn = composerDiv!.querySelector('button');
    expect(sendBtn).not.toBeNull();
    expect(sendBtn!).toBeDisabled();
  });

  it('baseline sanity: no card, no banner, and composer is enabled when pendingApprovals is empty', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    // Wait for isLoading -> false (the empty-state text appears).
    await screen.findByText('Чем могу помочь?');
    // No approval card region.
    expect(screen.queryByRole('region', { name: /Ожидает подтверждения/ })).not.toBeInTheDocument();
    // No expired banner.
    expect(
      screen.queryByText('Эта операция истекла — отправьте новое сообщение, чтобы продолжить.')
    ).not.toBeInTheDocument();
    // Composer input is enabled.
    const input = screen.getByPlaceholderText('Напишите сообщение...');
    expect(input).not.toBeDisabled();
  });

  it('expired path: ExpiredApprovalBanner renders and ToolApprovalCard does NOT', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [expiredBatch] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    await waitFor(() => {
      expect(
        screen.getByText('Эта операция истекла — отправьте новое сообщение, чтобы продолжить.')
      ).toBeInTheDocument();
    });
    // Card (pending path) is NOT rendered.
    expect(screen.queryByRole('region', { name: /Ожидает подтверждения/ })).not.toBeInTheDocument();
  });
});
