import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import axeCore from 'axe-core';
import { RegistrationPageContent as RegisterPage } from '../RegistrationPageContent';
import { api } from '@/lib/api';
import ru from '@/messages/ru.json';
import en from '@/messages/en.json';

const auth = vi.hoisted(() => ({ push: vi.fn(), setAuth: vi.fn() }));
vi.mock('@/lib/auth', () => ({ useAuthStore: () => auth.setAuth }));

vi.mock('@/lib/api', () => ({ api: { post: vi.fn() } }));
vi.mock('next/navigation', () => ({ useRouter: () => ({ push: auth.push }) }));
vi.mock('@/components/auth/AuthShell', () => ({
  AuthShell: ({
    children,
    title,
    description,
  }: {
    children: ReactNode;
    title: ReactNode;
    description: ReactNode;
  }) => (
    <div>
      <h1>{title}</h1>
      <p>{description}</p>
      {children}
    </div>
  ),
}));
beforeEach(() => {
  vi.mocked(api.post).mockReset();
  vi.stubEnv('REGISTRATION_MODE', undefined);
  auth.push.mockReset();
  auth.setAuth.mockReset();
});

afterEach(() => vi.unstubAllEnvs());

async function submit(invited = false) {
  const user = userEvent.setup();
  render(<RegisterPage invited={invited ? '1' : undefined} />);
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

it('shows the waitlist state for a direct visit in invite-only mode', () => {
  vi.stubEnv('REGISTRATION_MODE', 'invite_only');

  render(<RegisterPage />);

  expect(
    screen.getByRole('heading', { name: 'Подключаем организации небольшими группами.' })
  ).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Подать заявку' })).toHaveAttribute('href', '/#waitlist');
  expect(document.getElementById('email')).not.toBeInTheDocument();
});

it('shows the form from an invitation link while the API remains the authority', () => {
  vi.stubEnv('REGISTRATION_MODE', 'invite_only');

  render(<RegisterPage invited="1" />);

  expect(document.getElementById('email')).toBeInTheDocument();
});

it('maps an unapproved invitation email to the waitlist action', async () => {
  vi.mocked(api.post).mockRejectedValue({
    response: { status: 403, data: { code: 'registration_invite_required' } },
  });

  await submit();

  expect(
    await screen.findByText(
      'Для этой почты доступ пока не открыт. Подайте заявку или дождитесь приглашения на следующую волну.'
    )
  ).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Подать заявку на ранний доступ' })).toHaveAttribute(
    'href',
    '/#waitlist'
  );
});

it.each(['ru', 'en'] as const)('explains the invited email requirement in %s', (locale) => {
  (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
    locale
  );
  vi.stubEnv('REGISTRATION_MODE', 'invite_only');
  const copy = locale === 'ru' ? ru : en;
  render(<RegisterPage invited="1" />);
  expect(screen.getByText(copy.auth.register.closedBeta.invitedDescription)).toBeVisible();
  expect(screen.getByLabelText(copy.auth.register.emailLabel)).toBeVisible();
});

it.each(['ru', 'en'] as const)('keeps closed-beta actions usable in %s', (locale) => {
  (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
    locale
  );
  vi.stubEnv('REGISTRATION_MODE', 'invite_only');
  const copy = locale === 'ru' ? ru : en;
  render(<RegisterPage />);
  expect(
    screen.getByRole('link', { name: copy.auth.register.closedBeta.waitlistCta })
  ).toHaveAttribute('href', '/#waitlist');
  expect(
    screen.getByRole('link', { name: copy.auth.register.closedBeta.inviteCta })
  ).toHaveAttribute('href', '/register?invited=1');
  expect(
    screen.getByRole('link', { name: copy.auth.register.closedBeta.loginCta })
  ).toHaveAttribute('href', '/login');
});

it.each(['0', 'true', ['1', '1']])('does not treat %s as an invitation link', (invited) => {
  vi.stubEnv('REGISTRATION_MODE', 'invite_only');
  render(<RegisterPage invited={invited} />);
  expect(document.getElementById('email')).not.toBeInTheDocument();
});

it.each(['ru', 'en'] as const)(
  'shows actionable invitation errors without losing input in %s',
  async (locale) => {
    (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
      locale
    );
    vi.stubEnv('REGISTRATION_MODE', 'invite_only');
    const copy = locale === 'ru' ? ru : en;
    vi.mocked(api.post).mockRejectedValue({
      response: { status: 403, data: { code: 'registration_invite_required' } },
    });
    await submit(true);
    expect(await screen.findByRole('alert')).toHaveTextContent(copy.register.errors.inviteRequired);
    expect(screen.getByRole('link', { name: copy.register.errors.joinWaitlist })).toHaveAttribute(
      'href',
      '/#waitlist'
    );
    expect(screen.getByLabelText(copy.auth.register.emailLabel)).toHaveValue('owner@example.org');
    expect(auth.setAuth).not.toHaveBeenCalled();
    expect(auth.push).not.toHaveBeenCalled();
  }
);

it.each(['ru', 'en'] as const)(
  'allows retry after the invitation service is unavailable in %s',
  async (locale) => {
    (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
      locale
    );
    vi.stubEnv('REGISTRATION_MODE', 'invite_only');
    const copy = locale === 'ru' ? ru : en;
    vi.mocked(api.post).mockRejectedValue({
      response: { status: 503, data: { code: 'registration_gate_unavailable' } },
    });
    await submit(true);
    expect(await screen.findByRole('alert')).toHaveTextContent(
      copy.register.errors.gateUnavailable
    );
    expect(screen.getByRole('button', { name: copy.auth.register.submit })).toBeEnabled();
    expect(screen.getByLabelText(copy.auth.register.emailLabel)).toHaveValue('owner@example.org');
    expect(
      screen.queryByRole('link', { name: copy.register.errors.joinWaitlist })
    ).not.toBeInTheDocument();
    expect(auth.setAuth).not.toHaveBeenCalled();
  }
);

it('keeps open registration working and exposes pending state before the response', async () => {
  vi.stubEnv('REGISTRATION_MODE', 'open');
  let finish!: (value: unknown) => void;
  vi.mocked(api.post).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      })
  );
  await submit();
  const pending = screen.getByRole('button', { name: ru.auth.register.submitting });
  expect(pending).toBeDisabled();
  expect(pending).toHaveAttribute('aria-busy', 'true');
  const user = { id: 'new-user', email: 'owner@example.org', name: 'Owner' };
  await act(async () => finish({ data: { user, accessToken: 'test-token' } }));
  expect(auth.setAuth).toHaveBeenCalledWith(user, 'test-token');
  expect(auth.push).toHaveBeenCalledWith('/chat');
});

it('associates a duplicate-email error with the email field and focuses it', async () => {
  vi.mocked(api.post).mockRejectedValue({ response: { status: 409 } });
  await submit();
  const email = screen.getByLabelText(ru.auth.register.emailLabel);
  await waitFor(() => expect(email).toHaveAttribute('aria-invalid', 'true'));
  expect(email).toHaveAccessibleDescription(ru.auth.register.emailExists);
  expect(email).toHaveFocus();
});

it.each(['ru', 'en'] as const)(
  'has accessible closed-beta and invited-form controls in %s',
  async (locale) => {
    (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
      locale
    );
    vi.stubEnv('REGISTRATION_MODE', 'invite_only');
    const { container, rerender } = render(<RegisterPage />);
    const options = { rules: { region: { enabled: false }, 'color-contrast': { enabled: false } } };
    await act(async () => {
      expect((await axeCore.run(container, options)).violations).toEqual([]);
    });
    rerender(<RegisterPage invited="1" />);
    await act(async () => {
      expect((await axeCore.run(container, options)).violations).toEqual([]);
    });
  }
);
