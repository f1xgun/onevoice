import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import RegisterPage from '../page';
import { api } from '@/lib/api';

vi.mock('@/lib/api', () => ({ api: { post: vi.fn() } }));
vi.mock('next/navigation', () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock('@/components/auth/AuthShell', () => ({
  AuthShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
beforeEach(() => {
  vi.mocked(api.post).mockReset();
});

async function submit() {
  const user = userEvent.setup();
  render(<RegisterPage />);
  await user.type(document.getElementById('name')!, 'Owner');
  await user.type(document.getElementById('email')!, 'owner@example.org');
  await user.type(document.getElementById('password')!, '12345678');
  await user.type(document.getElementById('confirmPassword')!, '12345678');
  for (const checkbox of screen.getAllByRole('checkbox')) await user.click(checkbox);
  await user.click(document.querySelector('button[type="submit"]')!);
  await waitFor(() => expect(api.post).toHaveBeenCalledTimes(1));
}

it.each(['ru', 'en'] as const)(
  'maps password_too_weak to an accessible password error in %s',
  async (locale) => {
    (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
      locale
    );
    vi.mocked(api.post).mockRejectedValue({
      response: { status: 400, data: { code: 'password_too_weak' } },
    });
    await submit();
    const password = document.getElementById('password')!;
    await waitFor(() => expect(password).toHaveAttribute('aria-invalid', 'true'));
    expect(password).toHaveAccessibleDescription(
      locale === 'ru'
        ? 'Слишком простой пароль — используйте не менее 8 символов и выберите менее предсказуемый.'
        : 'Password is too weak — use at least 8 characters and choose something less predictable.'
    );
    expect(password).toHaveValue('12345678');
    expect(password).toHaveFocus();
  }
);

it.each([
  [
    400,
    { code: 'consent_required' },
    'Мы обновили документы — пожалуйста, перезагрузите страницу и подтвердите новые версии согласий.',
  ],
  [409, {}, 'Пользователь с таким email уже существует'],
  [400, { fields: { Password: 'Field policy error' } }, 'Field policy error'],
])('keeps other server validation branches separate (%s)', async (status, data, expected) => {
  vi.mocked(api.post).mockRejectedValue({ response: { status, data } });
  await submit();
  expect(screen.queryByText(/Слишком простой пароль/)).not.toBeInTheDocument();
  expect(await screen.findByText(expected as string)).toBeInTheDocument();
});
