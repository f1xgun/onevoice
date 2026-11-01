import { act, fireEvent, render, screen, waitFor, cleanup } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, expect, it, vi } from 'vitest';
import { useBusinessStore } from '@/lib/stores/business';
import ChatListPage from '../page';

const h = vi.hoisted(() => ({
  create: vi.fn(),
  list: vi.fn(),
  send: vi.fn(),
  push: vi.fn(),
  allowed: true,
}));
vi.mock('next/navigation', () => ({ useRouter: () => ({ push: h.push }) }));
vi.mock('@/lib/conversations', () => ({ listConversations: h.list, createConversation: h.create }));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: h.allowed, isError: false }),
}));
vi.mock('@/hooks/useConversationFlow', () => ({
  useConversationFlow: () => ({ messages: [], isStreaming: false, sendMessage: h.send }),
}));
vi.mock('@/components/templates/TemplateChips', () => ({
  TemplateChips: ({ onPrefill }: { onPrefill: (text: string) => void }) => (
    <button onClick={() => onPrefill('Editable occasion')}>Новинка в меню</button>
  ),
}));
vi.mock('@/components/chat/ChatPhotoInput', () => ({
  ChatPhotoInput: ({ onChange }: { onChange: (photo: unknown) => void }) => (
    <button
      type="button"
      onClick={() =>
        onChange({
          type: 'image',
          url: '/media/photo.png',
          name: 'Photo.png',
          mimeType: 'image/png',
        })
      }
    >
      Фото
    </button>
  ),
}));
function show() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <ChatListPage />
    </QueryClientProvider>
  );
}
beforeEach(() => {
  useBusinessStore.setState({ activeBusinessId: 'business' });
  h.allowed = true;
  h.push.mockReset();
  h.create.mockReset().mockResolvedValue({ id: 'created' });
  h.list.mockReset().mockResolvedValue([]);
  h.send.mockReset().mockImplementation(async (_text, accepted) => accepted?.());
  window.history.replaceState({}, '', '/chat');
});
it('keeps New/Repeat post text editable without creating an empty conversation on visit or refresh', async () => {
  window.history.replaceState({}, '', '/chat?prompt=Prepared+post&businessId=business');
  const page = show();
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue('Prepared post'));
  page.unmount();
  show();
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue('Prepared post'));
  expect(h.create).not.toHaveBeenCalled();
  expect(h.send).not.toHaveBeenCalled();
});
it('creates and sends exactly once only after explicit Send, including a separately uploaded photo', async () => {
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Новинка в меню' }));
  expect(h.create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Фото' }));
  fireEvent.click(screen.getByRole('button', { name: 'Поручить' }));
  await waitFor(() => expect(h.send).toHaveBeenCalledTimes(1));
  expect(h.create).toHaveBeenCalledTimes(1);
  expect(h.send).toHaveBeenCalledWith('Editable occasion', expect.any(Function), {
    conversationId: 'created',
    attachments: [
      { type: 'image', url: '/media/photo.png', name: 'Photo.png', mimeType: 'image/png' },
    ],
  });
  expect(h.push).toHaveBeenCalledWith('/chat/created');
});
it('does not send or navigate when the business changes during conversation creation', async () => {
  let finish!: (value: { id: string }) => void;
  h.create.mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  show();
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Old business text' } });
  fireEvent.click(screen.getByRole('button', { name: 'Поручить' }));
  await waitFor(() => expect(h.create).toHaveBeenCalledOnce());
  await act(async () => {
    useBusinessStore.setState({ activeBusinessId: 'other' });
    finish({ id: 'old' });
  });
  expect(h.send).not.toHaveBeenCalled();
  expect(h.push).not.toHaveBeenCalled();
});
it('reuses the created conversation on an explicit retry and preserves the instruction', async () => {
  h.send.mockResolvedValue(undefined);
  show();
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Keep this instruction' } });
  fireEvent.click(screen.getByRole('button', { name: 'Поручить' }));
  await waitFor(() => expect(h.send).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Поручить' })).toBeEnabled());
  expect(screen.getByRole('textbox')).toHaveValue('Keep this instruction');
  fireEvent.click(screen.getByRole('button', { name: 'Поручить' }));
  await waitFor(() => expect(h.send).toHaveBeenCalledTimes(2));
  expect(h.create).toHaveBeenCalledOnce();
});
it('keeps history available without create permission and never creates project folders', async () => {
  h.allowed = false;
  h.list.mockResolvedValue([
    {
      id: 'old',
      title: 'Earlier instruction',
      titleStatus: 'manual',
      preview: 'Published on the platform',
      createdAt: '2026-10-30T08:00:00Z',
      projectId: 'legacy',
    },
  ]);
  show();
  expect(await screen.findByRole('link', { name: /Earlier instruction/ })).toHaveAttribute(
    'href',
    '/chat/old'
  );
  expect(screen.getByRole('textbox')).toBeDisabled();
  expect(screen.queryByText('Без проекта')).not.toBeInTheDocument();
  expect(h.create).not.toHaveBeenCalled();
});
it('retains retryable history errors', async () => {
  h.list.mockRejectedValueOnce(new Error('offline')).mockResolvedValue([]);
  show();
  expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось');
  fireEvent.click(screen.getByRole('button', { name: 'Повторить' }));
  expect(await screen.findByText(/Пока нет поручений/)).toBeInTheDocument();
  cleanup();
});

it('hides empty legacy rows and cleans an untitled instruction preview without inventing its content', async () => {
  h.list.mockResolvedValue([
    {
      id: 'empty',
      title: 'Новый чат',
      titleStatus: 'auto_pending',
      createdAt: '2026-10-30T08:00:00Z',
      lastMessageAt: '2026-10-30T08:00:00Z',
    },
    {
      id: 'real',
      title: '',
      titleStatus: 'auto_pending',
      preview: '**Done**: [original words](https://example.org)',
      createdAt: '2026-10-30T08:00:00Z',
    },
  ]);
  show();
  expect(
    await screen.findByRole('link', { name: /Поручение Done: original words/ })
  ).toHaveAttribute('href', '/chat/real');
  expect(screen.queryByRole('link', { name: /Новый чат/ })).not.toBeInTheDocument();
  expect(screen.queryByText(/\*\*Done/)).not.toBeInTheDocument();
});
