import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, expect, it, vi } from 'vitest';
import ChatListPage from '../page';
import { listConversations } from '@/lib/conversations';
import { bizApi } from '@/lib/api/business-api';

const h = vi.hoisted(() => ({
  permissions: ['content.create', 'content.update', 'content.delete'],
  projects: [
    { id: 'summer', name: 'Летняя акция' },
    { id: 'winter', name: 'Зимнее меню' },
  ],
  projectsPending: false,
  projectsError: false,
  retryProjects: vi.fn(),
  push: vi.fn(),
  post: vi.fn(),
}));
vi.mock('next/navigation', () => ({ useRouter: () => ({ push: h.push }) }));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: (permission: string) => ({ allowed: h.permissions.includes(permission) }),
}));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (select: (state: { activeBusinessId: string }) => unknown) =>
    select({ activeBusinessId: 'business' }),
}));
vi.mock('@/lib/conversations', () => ({ listConversations: vi.fn() }));
vi.mock('@/lib/api/business-api', () => ({ bizApi: vi.fn() }));
vi.mock('@/lib/telemetry', () => ({ trackClick: vi.fn() }));
vi.mock('@/hooks/useProjects', () => ({
  useProjectsQuery: () => ({
    data: h.projects,
    isPending: h.projectsPending,
    isError: h.projectsError,
    refetch: h.retryProjects,
  }),
}));

const chats = [
  { id: 'unassigned', title: 'Время работы', projectId: null, preview: 'Открываемся в десять' },
  { id: 'summer-chat', title: 'Пост об акции', projectId: 'summer', preview: '**Скидка** на кофе' },
  { id: 'winter-chat', title: 'Обновить меню', projectId: 'winter', preview: 'Добавим какао' },
  { id: 'orphan-chat', title: 'Старый проект', projectId: 'missing', preview: '' },
].map((chat) => ({
  ...chat,
  userId: 'user',
  businessId: 'business',
  createdAt: '2026-09-08T12:00:00Z',
  updatedAt: '2026-09-08T12:00:00Z',
}));
function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ChatListPage />
    </QueryClientProvider>
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  h.permissions = ['content.create', 'content.update', 'content.delete'];
  h.projectsPending = false;
  h.projectsError = false;
  vi.mocked(listConversations).mockResolvedValue(chats);
  vi.mocked(bizApi).mockReturnValue({ post: h.post } as unknown as ReturnType<typeof bizApi>);
});

it('groups every chat by project and preserves unassigned and unavailable project buckets', async () => {
  renderPage();
  const unassigned = await screen.findByRole('region', { name: 'Без проекта' });
  expect(within(unassigned).getByText('Время работы')).toBeVisible();
  expect(
    within(screen.getByRole('region', { name: 'Летняя акция' })).getByText('Пост об акции')
  ).toBeVisible();
  expect(
    within(screen.getByRole('region', { name: 'Зимнее меню' })).getByText('Обновить меню')
  ).toBeVisible();
  expect(
    within(screen.getByRole('region', { name: 'Проект недоступен' })).getByText('Старый проект')
  ).toBeVisible();
  expect(screen.getByText('Скидка').tagName).toBe('STRONG');
  await userEvent.click(screen.getByRole('button', { name: /Летняя акция/ }));
  expect(screen.queryByText('Пост об акции')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: /Летняя акция/ })).toHaveAttribute(
    'aria-expanded',
    'false'
  );
});

it('searches project names, chat titles and previews, reveals collapsed results and offers a reset', async () => {
  renderPage();
  const search = await screen.findByRole('searchbox');
  await userEvent.click(screen.getByRole('button', { name: /Летняя акция/ }));
  await userEvent.type(search, 'летняя');
  expect(screen.getByText('Пост об акции')).toBeVisible();
  expect(screen.queryByText('Обновить меню')).not.toBeInTheDocument();
  await userEvent.clear(search);
  await userEvent.type(search, 'какао');
  expect(screen.getByText('Обновить меню')).toBeVisible();
  await userEvent.clear(search);
  await userEvent.type(search, 'время');
  expect(screen.getByText('Время работы')).toBeVisible();
  await userEvent.clear(search);
  await userEvent.type(search, 'неизвестное');
  await userEvent.click(screen.getByRole('button', { name: 'Сбросить поиск' }));
  expect(search).toHaveValue('');
  expect(screen.getByText('Обновить меню')).toBeVisible();
});

it('does not flash projectless results while project names load and offers retry after a project failure', async () => {
  h.projectsPending = true;
  const view = renderPage();
  await act(async () => {});
  expect(screen.queryByText('Проект недоступен')).not.toBeInTheDocument();
  expect(screen.queryByText('Время работы')).not.toBeInTheDocument();
  view.unmount();
  h.projectsPending = false;
  h.projectsError = true;
  renderPage();
  await userEvent.click(await screen.findByRole('button', { name: 'Повторить' }));
  expect(h.retryProjects).toHaveBeenCalledTimes(1);
  expect(screen.queryByText('Нет чатов')).not.toBeInTheDocument();
});

it('creates from the empty CTA and shows progress before the request resolves', async () => {
  vi.mocked(listConversations).mockResolvedValue([]);
  let resolveCreate!: (value: { data: { id: string } }) => void;
  h.post.mockImplementation(
    () =>
      new Promise((resolve) => {
        resolveCreate = resolve;
      })
  );
  renderPage();
  await userEvent.click(await screen.findByRole('button', { name: 'Начать первый чат' }));
  await waitFor(() =>
    expect(screen.getAllByRole('button', { name: 'Создаём чат…' })).toHaveLength(2)
  );
  for (const button of screen.getAllByRole('button', { name: 'Создаём чат…' })) {
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('aria-busy', 'true');
  }
  expect(h.push).not.toHaveBeenCalled();
  await act(async () => resolveCreate({ data: { id: 'new-chat' } }));
  await waitFor(() => expect(h.push).toHaveBeenCalledWith('/chat/new-chat'));
  expect(h.post).toHaveBeenCalledTimes(1);
});

it('hides create CTA and row mutation menus when permission is absent while preserving read access', async () => {
  h.permissions = [];
  const view = renderPage();
  expect(await screen.findByText('Время работы')).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Новый чат' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Меню чата/ })).not.toBeInTheDocument();
  view.unmount();
  vi.mocked(listConversations).mockResolvedValue([]);
  renderPage();
  expect(await screen.findByText(/Здесь появятся чаты вашей команды/)).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Начать первый чат' })).not.toBeInTheDocument();
});
