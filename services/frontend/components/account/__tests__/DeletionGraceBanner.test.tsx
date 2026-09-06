import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const restoreAccountMock = vi.fn();
vi.mock('@/lib/api/account', () => ({
  restoreAccount: () => restoreAccountMock(),
}));

const toastErrorMock = vi.fn();
vi.mock('sonner', () => ({
  toast: { error: (m: string) => toastErrorMock(m), success: vi.fn() },
}));

vi.mock('@/lib/auth', () => ({
  useAuthStore: (selector?: (s: unknown) => unknown) => {
    const state = {
      user: { accountDeletion: { scheduledDeletionAt: '2026-10-06T00:00:00Z' } },
    };
    return selector ? selector(state) : state;
  },
}));

import { DeletionGraceBanner } from '@/components/account/DeletionGraceBanner';

async function clickRestore(): Promise<void> {
  const user = userEvent.setup();
  render(<DeletionGraceBanner />);
  await user.click(screen.getByRole('button', { name: 'Отменить удаление аккаунта' }));
}

describe('DeletionGraceBanner restore failures', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it.each([
    {
      name: 'origin_not_allowed 403',
      error: { code: 'origin_not_allowed', status: 403 },
      expected: 'Запрос отклонён по соображениям безопасности.',
    },
    {
      name: 'backend 500',
      error: { code: 'internal_error', status: 500 },
      expected: 'Что-то пошло не так. Попробуйте ещё раз через минуту.',
    },
    {
      name: 'network failure with no code',
      error: { status: 0 },
      expected: 'Что-то пошло не так. Попробуйте ещё раз через минуту.',
    },
  ])('never claims an expired window for $name', async ({ error, expected }) => {
    restoreAccountMock.mockRejectedValue(error);

    await clickRestore();

    await waitFor(() => {
      expect(toastErrorMock).toHaveBeenCalledWith(expected);
    });
    expect(toastErrorMock).not.toHaveBeenCalledWith('Срок отмены истёк. Аккаунт уже удалён.');
  });

  it('keeps the terminal copy for deletion_too_old', async () => {
    restoreAccountMock.mockRejectedValue({ code: 'deletion_too_old', status: 410 });

    await clickRestore();

    await waitFor(() => {
      expect(toastErrorMock).toHaveBeenCalledWith('Срок отмены истёк. Аккаунт уже удалён.');
    });
  });
});
