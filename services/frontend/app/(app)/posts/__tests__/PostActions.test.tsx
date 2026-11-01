import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PostActions } from '../_components/PostActions';
import type { Post } from '@/types/post';

const state = vi.hoisted(() => ({
  owner: true,
  create: true,
  edit: true,
  permissionError: false,
  membershipError: false,
  loading: false,
  refetch: vi.fn(),
  membershipRefetch: vi.fn(),
  patch: vi.fn(),
  remove: vi.fn(),
  retry: vi.fn(),
}));
vi.mock('@/lib/stores/business', () => ({ useBusinessStore: () => 'business' }));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: (permission: string) => ({
    allowed:
      permission === 'content.create'
        ? state.create
        : permission === 'content.update'
          ? state.edit
          : true,
    isError: state.permissionError,
    isLoading: state.loading,
    refetch: state.refetch,
  }),
}));
vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => ({
    isError: state.membershipError,
    isLoading: false,
    refetch: state.membershipRefetch,
    data: [
      {
        id: 'business',
        role: { id: state.owner ? '00000000-0000-0000-0000-000000000001' : 'admin' },
      },
    ],
  }),
}));
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({ patch: state.patch, delete: state.remove, post: state.retry }),
}));
const published: Post = {
  id: 'published',
  businessId: 'business',
  content: 'Original',
  status: 'published',
  createdAt: '2026-10-30',
  platformResults: { telegram: { postId: '1', externalId: '-1001', url: '', status: 'published' } },
};
const failed: Post = {
  ...published,
  id: 'failed',
  status: 'error',
  platformResults: {
    vk: { postId: '', externalId: '-1002', url: '', status: 'error', error: 'Expired access' },
  },
};
function show(posts: Post[]) {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <PostActions posts={posts} />
    </QueryClientProvider>
  );
}
beforeEach(() => {
  state.owner = true;
  state.create = true;
  state.edit = true;
  state.permissionError = false;
  state.membershipError = false;
  state.loading = false;
  state.refetch.mockReset();
  state.membershipRefetch.mockReset();
  state.patch.mockReset().mockResolvedValue({ data: published });
  state.remove.mockReset().mockResolvedValue({ data: { ...published, status: 'deleted' } });
  state.retry
    .mockReset()
    .mockResolvedValue({ data: { ...failed, status: 'published', platformResults: {} } });
});
it('retries only the failed persisted member of a partial broadcast', async () => {
  show([published, failed]);
  fireEvent.click(screen.getByRole('button', { name: /Отправить в/ }));
  await waitFor(() => expect(state.retry).toHaveBeenCalledWith('/posts/failed/retry'));
  expect(state.patch).not.toHaveBeenCalled();
  expect(state.remove).not.toHaveBeenCalled();
});
it('requires explicit owner confirmation before deletion', async () => {
  show([published]);
  fireEvent.click(screen.getByRole('button', { name: /Удалить —/ }));
  expect(state.remove).not.toHaveBeenCalled();
  expect(screen.getByRole('dialog')).toHaveTextContent('Отменить это действие нельзя');
  fireEvent.click(screen.getByRole('button', { name: 'Удалить с площадки', exact: true }));
  await waitFor(() => expect(state.remove).toHaveBeenCalledWith('/posts/published'));
});
it('hides delete for nonowners and lifecycle actions for unverified and uncertain rows', () => {
  state.owner = false;
  show([
    { ...published, status: 'uncertain' },
    {
      ...failed,
      platformResults: { vk: { ...failed.platformResults!.vk, externalId: undefined } },
    },
  ]);
  expect(
    screen.queryByRole('button', { name: /Удалить|Исправить|Отправить в/ })
  ).not.toBeInTheDocument();
});
it('edits the chosen persisted post without replacing media or other platform rows', async () => {
  show([published, failed]);
  fireEvent.click(screen.getByRole('button', { name: /Исправить —/ }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Текст поста' }), {
    target: { value: 'Corrected' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить на площадке' }));
  await waitFor(() =>
    expect(state.patch).toHaveBeenCalledWith('/posts/published', { content: 'Corrected' })
  );
  expect(state.retry).not.toHaveBeenCalled();
});

it.each([
  [false, true, false],
  [true, false, true],
])('gates retry on create permission: create=%s update=%s', (create, edit, visible) => {
  state.create = create;
  state.edit = edit;
  show([failed]);
  expect(!!screen.queryByRole('button', { name: /Отправить в/ })).toBe(visible);
});
it.each(['permission', 'membership'])('offers retry when the %s gate cannot load', (gate) => {
  state.permissionError = gate === 'permission';
  state.membershipError = gate === 'membership';
  show([published]);
  fireEvent.click(within(screen.getByRole('alert')).getByRole('button'));
  expect(state.refetch).toHaveBeenCalledOnce();
  expect(state.membershipRefetch).toHaveBeenCalledOnce();
  expect(screen.queryByRole('button', { name: /Удалить —|Исправить —/ })).not.toBeInTheDocument();
});
it('shows the pending permission check before presenting actions', () => {
  state.loading = true;
  show([published]);
  expect(screen.getByRole('status')).toHaveTextContent('Загрузка');
  expect(screen.queryByRole('button', { name: /Удалить —|Исправить —/ })).not.toBeInTheDocument();
});
it('reschedules a queued Telegram post with the same persisted id', async () => {
  const date = new Date(Date.now() + 86_400_000);
  const scheduled = {
    ...published,
    status: 'scheduled',
    scheduledAt: date.toISOString(),
    platformResults: {
      telegram: { ...published.platformResults!.telegram, postId: '', status: 'scheduled' },
    },
  };
  show([scheduled]);
  fireEvent.click(screen.getByRole('button', { name: 'Исправить текст и время' }));
  const next = new Date(Date.now() + 172_800_000);
  const local = new Date(next.getTime() - next.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16);
  fireEvent.change(screen.getByLabelText('Когда публиковать'), { target: { value: local } });
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить на площадке' }));
  await waitFor(() =>
    expect(state.patch).toHaveBeenCalledWith('/posts/published', {
      content: 'Original',
      scheduledAt: new Date(local).toISOString(),
    })
  );
});
it('requires owner confirmation to cancel a queued post', async () => {
  const scheduled = {
    ...published,
    status: 'scheduled',
    scheduledAt: '2099-01-01T12:00:00Z',
    platformResults: {
      telegram: { ...published.platformResults!.telegram, postId: '', status: 'scheduled' },
    },
  };
  show([scheduled]);
  fireEvent.click(screen.getByRole('button', { name: 'Снять с публикации', exact: true }));
  expect(state.remove).not.toHaveBeenCalled();
  expect(screen.getByRole('dialog')).toHaveTextContent('Пост не будет отправлен');
  fireEvent.click(
    within(screen.getByRole('dialog')).getByRole('button', {
      name: 'Снять с публикации',
      exact: true,
    })
  );
  await waitFor(() => expect(state.remove).toHaveBeenCalledWith('/posts/published'));
});
