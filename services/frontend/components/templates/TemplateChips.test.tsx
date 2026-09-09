import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ComponentProps, ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { TemplateChips } from './TemplateChips';

const BUSINESS_ID = '11111111-1111-4111-8111-111111111111';
const TEMPLATE_ID = '22222222-2222-4222-8222-222222222222';
const template = {
  id: TEMPLATE_ID,
  businessId: BUSINESS_ID,
  createdBy: '33333333-3333-4333-8333-333333333333',
  name: 'Приветствие',
  kind: 'post' as const,
  body: 'Здравствуйте, {name}',
  placeholders: ['name'],
  createdAt: '2026-09-09T00:00:00Z',
  updatedAt: '2026-09-09T00:00:00Z',
};
const getMock = vi.fn();
const postMock = vi.fn();
const putMock = vi.fn();
const deleteMock = vi.fn();

vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({ get: getMock, post: postMock, put: putMock, delete: deleteMock }),
}));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false, isError: false }),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function renderChips(props?: Partial<ComponentProps<typeof TemplateChips>>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onPrefill = vi.fn();
  render(
    <TemplateChips
      businessId={BUSINESS_ID}
      conversationKey="chat-a"
      disabled={false}
      currentInput=""
      onPrefill={onPrefill}
      {...props}
    />,
    { wrapper: wrapper(client) }
  );
  return { onPrefill };
}

describe('TemplateChips', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getMock.mockResolvedValue({ data: [template] });
  });

  it('labels the library editor and explains placeholder syntax', async () => {
    const user = userEvent.setup();
    renderChips();
    await user.click(await screen.findByRole('button', { name: 'Шаблоны (1)' }));
    const dialog = screen.getByRole('dialog', { name: 'Библиотека шаблонов организации' });
    expect(within(dialog).getByText(/Создавайте тексты публикаций/)).toBeInTheDocument();
    await user.click(within(dialog).getByRole('button', { name: 'Новый шаблон' }));
    expect(within(dialog).getByRole('textbox', { name: 'Название шаблона' })).toBeInTheDocument();
    expect(within(dialog).getByRole('combobox', { name: 'Тип шаблона' })).toBeInTheDocument();
    expect(within(dialog).getByRole('textbox', { name: 'Текст шаблона' })).toBeInTheDocument();
    expect(
      within(dialog).getByRole('textbox', { name: 'Объявленные переменные' })
    ).toBeInTheDocument();
    expect(within(dialog).getByText(/Удваивайте каждую скобку/)).toBeInTheDocument();
  });

  it('serializes renders and prefills only after the response', async () => {
    const pending = deferred<{ data: { content: string } }>();
    postMock.mockReturnValue(pending.promise);
    const user = userEvent.setup();
    const { onPrefill } = renderChips();
    await user.click(await screen.findByRole('button', { name: 'Приветствие' }));
    const dialog = screen.getByRole('dialog', { name: 'Заполните «Приветствие»' });
    await user.type(within(dialog).getByRole('textbox', { name: 'name' }), 'Анна');
    await user.click(within(dialog).getByRole('button', { name: 'Добавить в сообщение' }));
    expect(within(dialog).getByRole('button', { name: 'Добавить в сообщение' })).toBeDisabled();
    pending.resolve({ data: { content: 'Здравствуйте, Анна' } });
    await waitFor(() => expect(onPrefill).toHaveBeenCalledWith('Здравствуйте, Анна'));
  });

  it('does not overwrite text typed while a render request is pending', async () => {
    const pending = deferred<{ data: { content: string } }>();
    postMock.mockReturnValue(pending.promise);
    getMock.mockResolvedValue({
      data: [{ ...template, name: 'Без полей', body: 'Готово', placeholders: [] }],
    });
    const user = userEvent.setup();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const onPrefill = vi.fn();
    const view = render(
      <TemplateChips
        businessId={BUSINESS_ID}
        conversationKey="chat-a"
        disabled={false}
        currentInput=""
        onPrefill={onPrefill}
      />,
      { wrapper: wrapper(client) }
    );
    await user.click(await screen.findByRole('button', { name: 'Без полей' }));
    view.rerender(
      <TemplateChips
        businessId={BUSINESS_ID}
        conversationKey="chat-a"
        disabled={false}
        currentInput="Пользователь уже печатает"
        onPrefill={onPrefill}
      />
    );
    pending.resolve({ data: { content: 'Готово' } });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Без полей' })).toBeEnabled());
    expect(onPrefill).not.toHaveBeenCalled();
  });

  it('keeps management available but prevents choosing a template for a disabled composer', async () => {
    const user = userEvent.setup();
    renderChips({ disabled: true });
    expect(await screen.findByRole('button', { name: 'Приветствие' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Шаблоны (1)' }));
    expect(
      screen.getByRole('dialog', { name: 'Библиотека шаблонов организации' })
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Приветствие' })).toBeDisabled();
  });

  it('wires create, update, and delete and locks the editor during a save', async () => {
    const user = userEvent.setup();
    postMock.mockResolvedValue({ data: template });
    deleteMock.mockResolvedValue({ data: null });
    const updatePending = deferred<{ data: typeof template }>();
    putMock.mockReturnValue(updatePending.promise);
    renderChips();
    await user.click(await screen.findByRole('button', { name: 'Шаблоны (1)' }));
    const dialog = screen.getByRole('dialog', { name: 'Библиотека шаблонов организации' });
    await user.click(within(dialog).getByRole('button', { name: 'Новый шаблон' }));
    await user.type(
      within(dialog).getByRole('textbox', { name: 'Название шаблона' }),
      'Без переменных'
    );
    await user.type(
      within(dialog).getByRole('textbox', { name: 'Текст шаблона' }),
      'Готовый текст'
    );
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить' }));
    await waitFor(() =>
      expect(postMock).toHaveBeenCalledWith(
        '/content-templates',
        expect.objectContaining({ name: 'Без переменных' })
      )
    );
    await user.click(within(dialog).getByRole('button', { name: 'Изменить' }));
    const name = within(dialog).getByRole('textbox', { name: 'Название шаблона' });
    await user.clear(name);
    await user.type(name, 'Обновлённый');
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить' }));
    expect(name).toBeDisabled();
    expect(within(dialog).getByRole('button', { name: 'Новый шаблон' })).toBeDisabled();
    expect(within(dialog).getByRole('button', { name: 'Изменить' })).toBeDisabled();
    updatePending.resolve({ data: { ...template, name: 'Обновлённый' } });
    await waitFor(() => expect(putMock).toHaveBeenCalled());
    await user.click(within(dialog).getByRole('button', { name: 'Удалить' }));
    await waitFor(() =>
      expect(deleteMock).toHaveBeenCalledWith(`/content-templates/${TEMPLATE_ID}`)
    );
  });
});
