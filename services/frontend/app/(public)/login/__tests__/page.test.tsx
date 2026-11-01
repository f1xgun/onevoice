import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import LoginPage from '../page';
import { api } from '@/lib/api';

const auth = vi.hoisted(() => ({ push: vi.fn(), setAuth: vi.fn() }));
vi.mock('next/navigation', () => ({ useRouter: () => ({ push: auth.push, refresh: vi.fn() }) }));
vi.mock('@/lib/auth', () => ({ useAuthStore: () => auth.setAuth }));
vi.mock('@/lib/api', () => ({ api: { post: vi.fn() } }));

beforeEach(() => {
  vi.mocked(api.post).mockReset();
  auth.push.mockReset();
  auth.setAuth.mockReset();
  window.history.replaceState({}, '', '/login');
});

async function submit() {
  const user = userEvent.setup();
  render(<LoginPage />);
  await user.type(screen.getByLabelText('Почта'), 'owner@example.org');
  await user.type(screen.getByLabelText('Пароль'), 'password123');
  await user.click(screen.getByRole('button', { name: 'Войти', exact: true }));
}

describe('login access screen', () => {
  it('associates the server error with both fields, marks the password invalid and keeps input', async () => {
    vi.mocked(api.post).mockRejectedValue({ response: { status: 401 } });
    await submit();
    const error = await screen.findByRole('alert');
    for (const label of ['Почта', 'Пароль']) {
      const field = screen.getByLabelText(label);
      expect(field).toHaveAccessibleDescription(error.textContent!);
    }
    expect(screen.getByLabelText('Почта')).toHaveAttribute('aria-invalid', 'false');
    expect(screen.getByLabelText('Пароль')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('Почта')).toHaveValue('owner@example.org');
    expect(auth.setAuth).not.toHaveBeenCalled();
    expect(auth.push).not.toHaveBeenCalled();
  });

  it('preserves the invitation destination after successful login', async () => {
    window.history.replaceState({}, '', '/login?next=/invite/token');
    const user = { id: 'u1', name: 'Owner', email: 'owner@example.org' };
    vi.mocked(api.post).mockResolvedValue({ data: { user, accessToken: 'token' } });
    await submit();
    await waitFor(() => expect(auth.setAuth).toHaveBeenCalledWith(user, 'token'));
    expect(auth.push).toHaveBeenCalledWith('/invite/token');
  });
});
