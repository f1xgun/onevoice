import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { ChatWindow } from '../ChatWindow';
import { useAuthStore } from '@/lib/auth';
import { singleCallBatch, expiredBatch } from '@/test-utils/pending-approval-fixtures';

// Mock sonner so toast.error from unrelated flows is inert.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const permState = vi.hoisted(() => ({ allowed: true }));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: permState.allowed, isLoading: false }),
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

function mockGetMessagesStatus(status: number) {
  const fetchMock = vi.fn().mockImplementation(async () => new Response('{}', { status }));
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

  it('gives the message composer an accessible name', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    expect(await screen.findByRole('textbox', { name: 'Напишите сообщение…' })).toBeInTheDocument();
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

describe('ChatWindow — history load failure', () => {
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

  it('shows a load error with retry (not the empty onboarding state) when the messages GET fails', async () => {
    mockGetMessagesStatus(500);
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    await screen.findByText(
      'Не удалось загрузить данные. Обновите страницу или попробуйте ещё раз.'
    );
    expect(screen.getByRole('button', { name: 'Повторить' })).toBeInTheDocument();
    expect(screen.queryByText('Чем могу помочь?')).not.toBeInTheDocument();
    const input = screen.getByPlaceholderText('Напишите сообщение…');
    expect(input).toBeDisabled();
  });
});

describe('ChatWindow — read-only viewer', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    permState.allowed = false;
    useAuthStore.setState({
      user: null,
      accessToken: 'test-token',
      isAuthenticated: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    permState.allowed = true;
  });

  it('shows the read-only hint and disables the composer when the viewer lacks content.create', async () => {
    mockGetMessages({ messages: [], pendingApprovals: [] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    await screen.findByText('У вас нет прав отправлять сообщения в этой организации');
    expect(screen.getByPlaceholderText('Напишите сообщение…')).toBeDisabled();
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

describe('composer keyboard and failed submission', () => {
  it('keeps Enter multiline, ignores IME, and preserves text after a rejected POST', async () => {
    const fetchMock = mockGetMessages({ messages: [], pendingApprovals: [] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    await screen.findByText('Чем могу помочь?');
    const input = screen.getByRole('textbox', { name: 'Напишите сообщение…' });
    expect(input.tagName).toBe('TEXTAREA');
    fireEvent.change(input, { target: { value: 'Первая строка\nВторая строка' } });
    fetchMock.mockClear();
    fireEvent.keyDown(input, { key: 'Enter' });
    fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true, isComposing: true });
    expect(fetchMock).not.toHaveBeenCalled();
    fetchMock.mockResolvedValue(new Response('{}', { status: 503 }));
    fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true });
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    await waitFor(() => expect(input).not.toBeDisabled());
    expect(input).toHaveValue('Первая строка\nВторая строка');
  });
});

it.each([
  { frame: 'data: {"type":"done"}\n\n', cleared: true },
  {
    frame:
      'data: {"type":"text","content":"Подготовлен пост"}\n\n' +
      'data: {"type":"tool_approval_required","batch_id":"b1","calls":[{"call_id":"c1","tool_name":"telegram__send_channel_post","args":{"text":"Публикация"},"editable_fields":["text"],"floor":"manual"}]}\n\n',
    cleared: true,
  },
  { frame: 'data: {"type":"text","content":"Частичный ответ"}\n\n', cleared: false },
  { frame: '', cleared: false },
  {
    frame: 'data: {"type":"error","code":"max_iterations"}\n\ndata: {"type":"done"}\n\n',
    cleared: false,
  },
  { frame: 'data: {"type":"error","code":"max_iterations"}\n\n', cleared: false },
])(
  'clears the instruction only after successful streaming: $cleared',
  async ({ frame, cleared }) => {
    const fetchMock = mockGetMessages({ messages: [], pendingApprovals: [] });
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );
    await screen.findByText('Чем могу помочь?');
    const input = screen.getByRole('textbox', { name: 'Напишите сообщение…' });
    fireEvent.change(input, { target: { value: 'Проверить текст' } });
    fetchMock.mockResolvedValue(
      new Response(frame, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
    );
    fireEvent.keyDown(input, { key: 'Enter', metaKey: true });
    if (frame.includes('tool_approval_required')) {
      await screen.findByRole('region', { name: /Ожидает подтверждения/ });
      expect(input).toBeDisabled();
    } else {
      await waitFor(() => expect(input).not.toBeDisabled());
    }
    await waitFor(() => expect(input).toHaveValue(cleared ? '' : 'Проверить текст'));
    if (cleared) {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    }
    if (frame.includes('tool_approval_required')) {
      expect(screen.getByText('Подготовлен пост')).toBeInTheDocument();
      expect(screen.getByRole('region', { name: /Ожидает подтверждения/ })).toBeInTheDocument();
    }
    if (!frame || frame.includes('Частичный ответ')) {
      expect(
        await screen.findByText(
          'Ответ прервался до завершения. Попробуйте отправить поручение повторно.'
        )
      ).toBeInTheDocument();
      if (frame) expect(screen.getByText('Частичный ответ')).toBeInTheDocument();
      fetchMock.mockResolvedValue(new Response('data: {"type":"done"}\n\n'));
      fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true });
      await waitFor(() => expect(input).toHaveValue(''));
    }
  }
);
