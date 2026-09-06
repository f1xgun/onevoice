import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import NewBusinessPage from '../page';
import { joinWaitlist } from '@/lib/api/waitlist';
import { api } from '@/lib/api';
import { BUSINESS_LIST_QUERY_KEY } from '@/lib/hooks/useBusinessList';

vi.mock('@/lib/api/waitlist', () => ({ joinWaitlist: vi.fn().mockResolvedValue(undefined) }));
const push = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ push, back: vi.fn() }) }));
vi.mock('@/lib/api', () => ({ api: { post: vi.fn() } }));
beforeEach(() => {
  vi.mocked(api.post).mockReset();
  push.mockReset();
});

async function fillAndSubmit(cached: boolean) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  if (cached) client.setQueryData(BUSINESS_LIST_QUERY_KEY, [{ id: 'existing', name: 'Existing' }]);
  render(
    <QueryClientProvider client={client}>
      <NewBusinessPage />
    </QueryClientProvider>
  );
  const user = userEvent.setup();
  fireEvent.change(document.getElementById('name')!, { target: { value: 'New organization' } });
  await user.click(screen.getByRole('combobox'));
  await user.click(screen.getByRole('option', { name: /Кафе/ }));
  fireEvent.change(document.getElementById('address')!, { target: { value: 'My address' } });
  await user.click(screen.getByRole('button', { name: 'Создать' }));
  return user;
}

it.each([true, false])(
  'handles server Free limit independently of list cache (%s)',
  async (cached) => {
    vi.mocked(api.post).mockRejectedValue({
      response: { status: 403, data: { code: 'plan_limit_businesses' } },
    });
    const user = await fillAndSubmit(cached);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'На Free доступна одна организация.'
    );
    expect(document.getElementById('name')).toHaveValue('New organization');
    expect(document.getElementById('address')).toHaveValue('My address');
    expect(push).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Оставить заявку' }));
    const dialog = within(screen.getByRole('dialog'));
    fireEvent.change(dialog.getByRole('textbox'), { target: { value: 'owner@example.org' } });
    await user.click(dialog.getByRole('checkbox'));
    await user.click(dialog.getByRole('button', { name: 'Оставить заявку' }));
    await waitFor(() => expect(joinWaitlist).toHaveBeenCalled());
    expect(api.post).toHaveBeenCalledTimes(1);
    expect(document.getElementById('name')).toHaveValue('New organization');
  }
);

it('lets the server accept another organization without imposing a client list limit', async () => {
  vi.mocked(api.post).mockResolvedValue({ data: { id: 'new', name: 'New organization' } });
  await fillAndSubmit(true);
  await waitFor(() => expect(push).toHaveBeenCalledWith('/business'));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});
