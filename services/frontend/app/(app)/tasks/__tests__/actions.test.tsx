import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, expect, it, vi } from 'vitest';
import type { AgentTask } from '@/types/task';
import TasksPage from '../page';

const requests = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false, isError: false }),
}));
vi.mock('@/lib/stores/business', () => ({ useBusinessStore: () => 'business-a' }));
vi.mock('@/lib/api/business-api', () => ({ bizApi: () => requests }));
vi.mock('@/hooks/useTasksStream', () => ({ useTasksStream: vi.fn() }));

const tasks: AgentTask[] = [
  {
    id: 'error',
    businessId: 'business-a',
    type: 'update',
    displayName: 'Неудачное обновление',
    status: 'error',
    platform: 'vk',
    createdAt: '2026-09-09T07:00:00Z',
  },
  {
    id: 'done',
    businessId: 'business-a',
    type: 'read',
    displayName: 'Успешная проверка',
    status: 'done',
    platform: 'telegram',
    createdAt: '2026-09-09T07:00:00Z',
    verificationStatus: 'mismatch',
    verificationCanRerun: true,
  },
];

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <TasksPage />
    </QueryClientProvider>
  );
  return client;
}

beforeEach(() => {
  requests.get.mockReset().mockResolvedValue({ data: tasks });
  requests.post.mockReset().mockResolvedValue({ data: {} });
});

it('drills into tasks needing help and provides a safe chat link without replaying an action', async () => {
  renderPage();
  await screen.findByText('Неудачное обновление');
  fireEvent.click(screen.getByRole('button', { name: /Разобрать ошибки/ }));
  expect(screen.getByText('Неудачное обновление')).toBeInTheDocument();
  expect(screen.queryByText('Успешная проверка')).not.toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Открыть чат' })).toHaveAttribute('href', '/chat');
  expect(requests.post).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Все задачи' }));
  expect(screen.getByText('Успешная проверка')).toBeInTheDocument();
});

it('shows the read-only verification request immediately and blocks duplicate clicks', async () => {
  let resolveRequest!: (value: { data: object }) => void;
  requests.post.mockReturnValue(
    new Promise((resolve) => {
      resolveRequest = resolve;
    })
  );
  renderPage();
  fireEvent.click(await screen.findByRole('button', { name: 'Проверить ещё раз' }));
  const pending = await screen.findByRole('button', { name: 'Ставим проверку…' });
  expect(pending).toBeDisabled();
  expect(pending).toHaveAttribute('aria-busy', 'true');
  fireEvent.click(pending);
  expect(requests.post).toHaveBeenCalledTimes(1);
  await act(async () => resolveRequest({ data: {} }));
});

it('offers reset on an empty category and a chat CTA when there are no tasks', async () => {
  requests.get.mockResolvedValue({ data: [] });
  renderPage();
  expect(await screen.findByRole('link', { name: 'Дать поручение в чате' })).toHaveAttribute(
    'href',
    '/chat'
  );
  fireEvent.click(screen.getByRole('button', { name: /Разобрать ошибки/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Показать все задачи' }));
  expect(screen.getByRole('link', { name: 'Дать поручение в чате' })).toBeInTheDocument();
});

it('does not replace a failed load with an empty-state invitation', async () => {
  requests.get.mockRejectedValue(new Error('Unavailable'));
  renderPage();
  await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
  expect(screen.queryByText('Задач пока нет')).not.toBeInTheDocument();
});
