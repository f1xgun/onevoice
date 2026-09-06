import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const deleteAccountMock = vi.fn();
vi.mock('@/lib/api/account', () => ({
  deleteAccount: (password: string) => deleteAccountMock(password),
}));

const toastErrorMock = vi.fn();
const toastSuccessMock = vi.fn();
vi.mock('sonner', () => ({
  toast: { error: (m: string) => toastErrorMock(m), success: (m: string) => toastSuccessMock(m) },
}));

import { DeleteConfirmModal } from '@/components/account/DeleteConfirmModal';

async function submitWithPassword(password: string): Promise<void> {
  const user = userEvent.setup();
  render(<DeleteConfirmModal open onOpenChange={vi.fn()} />);
  await user.type(screen.getByLabelText('Введите пароль для подтверждения'), password);
  await user.click(screen.getByRole('button', { name: 'Удалить через 30 дней' }));
}

describe('DeleteConfirmModal error copy', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the inline «Неверный пароль» only for password_invalid', async () => {
    deleteAccountMock.mockRejectedValue({ code: 'password_invalid', status: 401 });

    await submitWithPassword('correct-horse');

    await waitFor(() => {
      expect(screen.getByText('Неверный пароль')).toBeInTheDocument();
    });
    expect(toastErrorMock).not.toHaveBeenCalled();
  });

  it.each([
    {
      name: 'origin_not_allowed 403',
      error: { code: 'origin_not_allowed', status: 403 },
      expected: 'Запрос отклонён по соображениям безопасности.',
    },
    {
      name: 'unmapped backend 500',
      error: { code: 'internal_error', status: 500 },
      expected: 'Что-то пошло не так. Попробуйте ещё раз через минуту.',
    },
    {
      name: 'network failure with no code',
      error: { status: 0 },
      expected: 'Что-то пошло не так. Попробуйте ещё раз через минуту.',
    },
  ])('never claims a wrong password for $name', async ({ error, expected }) => {
    deleteAccountMock.mockRejectedValue(error);

    await submitWithPassword('correct-horse');

    await waitFor(() => {
      expect(toastErrorMock).toHaveBeenCalledWith(expected);
    });
    expect(toastErrorMock).not.toHaveBeenCalledWith('Неверный пароль');
    expect(screen.queryByText('Неверный пароль')).toBeNull();
  });
});
