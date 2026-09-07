import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import SettingsPage from '../page';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';

vi.mock('@/lib/api', () => ({ api: { patch: vi.fn(), put: vi.fn() } }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe('SettingsPage — editable display name', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      user: { id: 'u1', email: 'test@test.test', name: 'Old Name' },
      accessToken: 'tok',
      isAuthenticated: true,
    });
  });

  afterEach(() => cleanup());

  it('prefills the current name and saves a new one via PATCH /auth/profile', async () => {
    (api.patch as ReturnType<typeof vi.fn>).mockResolvedValue({ data: null });
    const user = userEvent.setup();
    render(<SettingsPage />, { wrapper });

    const input = screen.getByDisplayValue('Old Name');
    await user.clear(input);
    await user.type(input, 'New Name');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledWith('/auth/profile', { name: 'New Name' })
    );
    await waitFor(() => expect(useAuthStore.getState().user?.name).toBe('New Name'));
  });

  it('blocks save and shows an error for a too-short name', async () => {
    const user = userEvent.setup();
    render(<SettingsPage />, { wrapper });

    const input = screen.getByDisplayValue('Old Name');
    await user.clear(input);
    await user.type(input, 'A');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));

    expect(await screen.findByText('Минимум 2 символа')).toBeInTheDocument();
    expect(api.patch).not.toHaveBeenCalled();
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input).toHaveAccessibleDescription('Минимум 2 символа');
  });
});

it('keeps password errors beside the form and announces success only after the response', async () => {
  let finish!: (value: unknown) => void;
  vi.mocked(api.put).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    })
  );
  render(<SettingsPage />, { wrapper });
  const current = screen.getByLabelText('Текущий пароль');
  const next = screen.getByLabelText('Новый пароль');
  const confirmation = document.getElementById('confirmPassword')!;
  await userEvent.type(current, 'old-password');
  await userEvent.type(next, 'new-password');
  await userEvent.type(confirmation, 'new-password');
  await userEvent.click(screen.getByRole('button', { name: 'Изменить пароль' }));
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  await act(async () => finish({ data: {} }));
  expect(await screen.findByRole('status')).toHaveTextContent('Пароль изменён');
  expect(current).toHaveValue('');
});
