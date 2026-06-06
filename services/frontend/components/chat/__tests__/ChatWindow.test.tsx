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

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
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

describe('ChatWindow — HITL integration (Invariants 5 + 9)', () => {
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
    expect(screen.getByText('Проверьте аргументы перед выполнением')).toBeInTheDocument();
  });

  it('Invariant 9: composer input + Send button are HTML-disabled while pendingApproval is non-null', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [singleCallBatch] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    await screen.findByRole('region', { name: /Ожидает подтверждения/ });

    const input = screen.getByPlaceholderText('Напишите сообщение…');
    expect(input).toBeDisabled();

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
    await screen.findByText('Чем могу помочь?');
    expect(screen.queryByRole('region', { name: /Ожидает подтверждения/ })).not.toBeInTheDocument();
    expect(
      screen.queryByText('Эта операция истекла — отправьте новое сообщение, чтобы продолжить.')
    ).not.toBeInTheDocument();
    const input = screen.getByPlaceholderText('Напишите сообщение…');
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
    expect(screen.queryByRole('region', { name: /Ожидает подтверждения/ })).not.toBeInTheDocument();
  });
});

describe('ChatWindow — IntegrationTokenInvalidBanner detector', () => {
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

  it('renders banner when newest assistant message carries code=integration_token_invalid', async () => {
    mockGetMessages({
      messages: [
        {
          id: 'm1',
          role: 'user',
          content: 'отправь пост',
        },
        {
          id: 'm2',
          role: 'assistant',
          content: '',
          toolCalls: [{ id: 'call_x', name: 'telegram__send_channel_post', arguments: {} }],
          toolResults: [
            {
              toolCallId: 'call_x',
              content: { error: 'Unauthorized' },
              isError: true,
              code: 'integration_token_invalid',
            },
          ],
        },
      ],
      pendingApprovals: [],
    });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    expect(
      await screen.findByRole('link', { name: 'Переподключить Telegram' })
    ).toBeInTheDocument();
  });

  it('hides banner when no toolCall carries the code', async () => {
    mockGetMessages({
      messages: [
        {
          id: 'm1',
          role: 'user',
          content: 'hi',
        },
        {
          id: 'm2',
          role: 'assistant',
          content: 'готово',
          toolCalls: [{ id: 'call_x', name: 'telegram__send_channel_post', arguments: {} }],
          toolResults: [
            {
              toolCallId: 'call_x',
              content: { message_id: 1 },
              isError: false,
            },
          ],
        },
      ],
      pendingApprovals: [],
    });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    await screen.findByText('готово');
    expect(screen.queryByRole('link', { name: /Переподключить/ })).not.toBeInTheDocument();
  });

  it('ignores stale invalid_token in older assistant messages (only newest turn drives)', async () => {
    mockGetMessages({
      messages: [
        {
          id: 'm1',
          role: 'user',
          content: 'first',
        },
        {
          id: 'm2',
          role: 'assistant',
          content: '',
          toolCalls: [{ id: 'call_old', name: 'telegram__send_channel_post', arguments: {} }],
          toolResults: [
            {
              toolCallId: 'call_old',
              content: { error: 'Unauthorized' },
              isError: true,
              code: 'integration_token_invalid',
            },
          ],
        },
        {
          id: 'm3',
          role: 'user',
          content: 'second',
        },
        {
          id: 'm4',
          role: 'assistant',
          content: 'готово',
        },
      ],
      pendingApprovals: [],
    });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    await screen.findByText('готово');
    expect(screen.queryByRole('link', { name: /Переподключить/ })).not.toBeInTheDocument();
  });
});
