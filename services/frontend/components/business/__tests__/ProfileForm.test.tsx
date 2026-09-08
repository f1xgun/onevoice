import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import ru from '@/messages/ru.json';
import NewBusinessPage from '@/app/(app)/business/new/page';
import { ProfileForm } from '@/components/business/ProfileForm';
import type { Business } from '@/types/business';

const BIZ_ID = 'test-biz-id';

vi.mock('next/navigation', () => ({ useRouter: () => ({ push: vi.fn() }) }));

const saveProfile = vi.fn();

vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: vi.fn(),
    post: vi.fn(),
    put: (...args: unknown[]) => saveProfile(...args),
    patch: vi.fn(),
    delete: vi.fn(),
  }),
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: BIZ_ID }),
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('ProfileForm preserves unsaved edits across a BUSINESS_PROFILE refetch', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    saveProfile.mockResolvedValue({ data: {} });
  });

  it.each(['profile', 'new'])(
    'associates every %s caption with its control and focuses on caption click',
    async (page) => {
      const client = new QueryClient();
      const { container } = render(
        page === 'profile' ? (
          <ProfileForm defaultValues={{ name: 'Test', category: 'cafe' }} />
        ) : (
          <NewBusinessPage />
        ),
        {
          wrapper: wrapper(client),
        }
      );
      for (const id of ['name', 'address', 'phone', 'website', 'description', 'category']) {
        const input = container.querySelector(`#${id}`)!;
        const label = container.querySelector(`label[for="${id}"]`)!;
        expect(input).toHaveAccessibleName();
        expect(label).not.toBeNull();
        if (id !== 'category') {
          await userEvent.click(label);
          expect(input).toHaveFocus();
        }
      }
    }
  );

  it('keeps the typed name when defaultValues identity changes (e.g. post-logo-upload refetch)', async () => {
    const user = userEvent.setup();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    const initial: Partial<Business> = { name: 'Old', category: 'cafe' };
    const { rerender } = render(<ProfileForm defaultValues={initial} />, {
      wrapper: wrapper(client),
    });

    const nameInput = screen.getByPlaceholderText('Кофейня «Мята»') as HTMLInputElement;
    await user.clear(nameInput);
    await user.type(nameInput, 'New Draft');
    expect(nameInput.value).toBe('New Draft');

    rerender(
      <ProfileForm defaultValues={{ name: 'Old Refetched', category: 'cafe', logoUrl: 'x' }} />
    );

    expect((screen.getByPlaceholderText('Кофейня «Мята»') as HTMLInputElement).value).toBe(
      'New Draft'
    );
  });
});

it('announces saving only after a successful response and clears stale confirmation on edit', async () => {
  let finish!: (value: unknown) => void;
  saveProfile.mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  render(<ProfileForm defaultValues={{ name: 'Old', category: 'cafe' }} />, {
    wrapper: wrapper(new QueryClient()),
  });
  const name = screen.getByRole('textbox', { name: /Название/ });
  await userEvent.clear(name);
  await userEvent.type(name, 'Updated');
  await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  await act(async () => finish({ data: {} }));
  expect(await screen.findByRole('status')).toHaveTextContent('Данные сохранены');
  await userEvent.type(name, ' draft');
  await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument());
});

it('associates validation with the field and preserves edits on a failed save', async () => {
  saveProfile.mockRejectedValueOnce(new Error('offline'));
  render(<ProfileForm defaultValues={{ name: 'Old', category: 'cafe' }} />, {
    wrapper: wrapper(new QueryClient()),
  });
  const name = screen.getByRole('textbox', { name: /Название/ });
  await userEvent.clear(name);
  await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
  await waitFor(() => expect(name).toHaveAttribute('aria-invalid', 'true'));
  expect(name).toHaveAccessibleDescription();
  await userEvent.type(name, 'Draft');
  await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Не получилось сохранить');
  expect(name).toHaveValue('Draft');
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

it.each([
  { kind: 'custom', initial: 'Custom initial', draft: 'Custom initial draft' },
  { kind: 'preset', initial: 'cafe', draft: 'retail' },
])(
  'preserves $kind category edits made during saving and submits them on the next save',
  async ({ kind, initial, draft }) => {
    const user = userEvent.setup();
    let finish!: (value: unknown) => void;
    saveProfile.mockReset();
    saveProfile.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      })
    );
    const { rerender } = render(
      <ProfileForm defaultValues={{ name: 'Old', category: initial }} />,
      { wrapper: wrapper(new QueryClient()) }
    );
    const name = screen.getByRole('textbox', { name: /Название/ });
    await user.clear(name);
    await user.type(name, 'Updated');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(saveProfile).toHaveBeenCalledTimes(1));
    expect(name).toBeDisabled();
    expect(saveProfile.mock.calls[0][1]).toMatchObject({ name: 'Updated', category: initial });
    if (kind === 'custom') {
      await user.type(screen.getByDisplayValue(initial), ' draft');
    } else {
      await user.click(screen.getByRole('combobox', { name: 'Категория' }));
      await user.click(await screen.findByRole('option', { name: 'Розничная торговля' }));
    }
    await act(async () => finish({ data: {} }));
    if (kind === 'custom') {
      expect(
        screen.getByRole('textbox', { name: ru.business.profileForm.customCategoryAria })
      ).toHaveValue(draft);
    } else {
      expect(screen.getByRole('combobox', { name: 'Категория' })).toHaveTextContent(
        'Розничная торговля'
      );
    }
    expect(name).toHaveValue('Updated');
    expect(name).toBeEnabled();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeEnabled();
    rerender(<ProfileForm defaultValues={{ name: 'Updated', category: initial }} />);
    if (kind === 'custom') {
      expect(
        screen.getByRole('textbox', { name: ru.business.profileForm.customCategoryAria })
      ).toHaveValue(draft);
    } else {
      expect(screen.getByRole('combobox', { name: 'Категория' })).toHaveTextContent(
        'Розничная торговля'
      );
    }
    saveProfile.mockResolvedValueOnce({ data: {} });
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(saveProfile).toHaveBeenCalledTimes(2));
    expect(saveProfile.mock.calls[1][1]).toMatchObject({ name: 'Updated', category: draft });
    expect(await screen.findByRole('status')).toHaveTextContent('Данные сохранены');
  }
);

it('shows a visibly labelled custom category only after choosing Other', async () => {
  const user = userEvent.setup();
  render(<ProfileForm defaultValues={{ name: 'Test', category: 'cafe' }} />, {
    wrapper: wrapper(new QueryClient()),
  });
  expect(
    screen.queryByRole('textbox', { name: ru.business.profileForm.customCategoryAria })
  ).toBeNull();
  await user.click(screen.getByRole('combobox', { name: ru.business.profileForm.fields.category }));
  await user.click(screen.getByRole('option', { name: ru.business.categories.other }));
  const input = screen.getByRole('textbox', { name: ru.business.profileForm.customCategoryAria });
  expect(input).toHaveValue('');
  expect(input).toHaveAccessibleDescription(ru.business.profileForm.customCategoryHelp);
  await user.click(screen.getByText(ru.business.profileForm.customCategoryAria));
  expect(input).toHaveFocus();
  await user.type(input, 'Bakery');
  await user.click(screen.getByRole('combobox'));
  await user.click(screen.getByRole('option', { name: ru.business.categories.cafe }));
  expect(
    screen.queryByRole('textbox', { name: ru.business.profileForm.customCategoryAria })
  ).toBeNull();
});
