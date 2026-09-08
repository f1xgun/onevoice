import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { useBusinessStore } from '@/lib/stores/business';
import responses from '@/test/fixtures/channel-responses.json';

import IntegrationsPage from '../page';

const { getMock } = vi.hoisted(() => ({ getMock: vi.fn() }));
const searchParams = new URLSearchParams();
vi.mock('next/navigation', () => ({ useSearchParams: () => searchParams }));
vi.mock('@/lib/api', () => ({ api: { get: getMock } }));

let client: QueryClient;
let listResponse: unknown;
let listFailed: boolean;

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  useBusinessStore.getState().setActive('org');
  listResponse = responses.empty.body;
  listFailed = true;
  getMock.mockImplementation(async (path: string) => {
    if (path.endsWith('/integrations')) {
      if (listFailed)
        throw {
          response: { status: responses.listFailure.status, data: responses.listFailure.body },
        };
      return { data: listResponse };
    }
    if (path === '/platforms') return { data: [{ id: 'telegram', status: 'active' }] };
    if (path.endsWith('/me/permissions')) return { data: { permissions: [] } };
    return { data: {} };
  });
});

afterEach(() => {
  cleanup();
  client.clear();
  useBusinessStore.getState().clear();
  vi.clearAllMocks();
});

function renderPage() {
  return render(
    <QueryClientProvider client={client}>
      <IntegrationsPage />
    </QueryClientProvider>
  );
}

it('keeps a failed list request distinct from a channel failure and can retry to an empty list', async () => {
  renderPage();
  expect(await screen.findByText('Статус каналов пока неизвестен')).toBeVisible();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.queryByText('Не подключено')).not.toBeInTheDocument();
  listFailed = false;
  await userEvent.click(screen.getByRole('button', { name: 'Повторить' }));
  expect(await screen.findByText('Не подключено')).toBeVisible();
  expect(screen.queryByText('Статус каналов пока неизвестен')).not.toBeInTheDocument();
});

it('asks for an organization instead of showing an endless loading state', () => {
  useBusinessStore.getState().clear();
  renderPage();
  expect(screen.getByText('Выберите организацию, чтобы увидеть каналы')).toBeVisible();
  expect(screen.queryByText('Не подключено')).not.toBeInTheDocument();
});

it.each([null, {}, { data: [] }])('keeps an invalid list response %j unknown', async (body) => {
  listFailed = false;
  listResponse = body;
  renderPage();
  expect(await screen.findByText('Статус каналов пока неизвестен')).toBeVisible();
  expect(screen.queryByText('Не подключено')).not.toBeInTheDocument();
});

it('shows loading until the API returns instead of assuming channels are disconnected', () => {
  getMock.mockReturnValue(new Promise(() => {}));
  renderPage();
  expect(screen.getByRole('status', { name: 'Загружаем подключённые каналы' })).toHaveAttribute(
    'aria-busy',
    'true'
  );
  expect(screen.queryByText('Не подключено')).not.toBeInTheDocument();
  expect(screen.queryByText('Ошибка подключения')).not.toBeInTheDocument();
});
