import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'sonner';

import { TelegramConnectModal } from '../TelegramConnectModal';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const BUSINESS_ID = 'biz-1';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: BUSINESS_ID }),
}));

const apiPost = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: vi.fn(),
    post: (...args: unknown[]) => apiPost(...args),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  }),
}));

function renderModal(onClose = vi.fn()) {
  const utils = render(<TelegramConnectModal open={true} onClose={onClose} />);
  return { ...utils, onClose };
}

// Walks the two-step wizard: step 1 intro → "Далее", then types the channel
// id and clicks "Подключить".
async function submitChannel(user: ReturnType<typeof userEvent.setup>, channel: string) {
  await user.click(screen.getByRole('button', { name: 'Далее' }));
  await user.type(screen.getByPlaceholderText(/@channel/), channel);
  await user.click(screen.getByRole('button', { name: /^Подключить$/ }));
}

const ADMIN_FAIL = 'Ошибка подключения. Убедитесь, что бот добавлен как администратор в канал.';
const VERIFY_REQUIRED = 'Подтвердите email, чтобы продолжить.';

describe('TelegramConnectModal — connect flow', () => {
  beforeEach(() => {
    apiPost.mockReset();
    vi.mocked(toast.success).mockReset();
    vi.mocked(toast.error).mockReset();
  });

  it('POSTs the trimmed channel id and shows success', async () => {
    apiPost.mockResolvedValueOnce({ data: {} });
    const user = userEvent.setup();
    const { onClose } = renderModal();

    await submitChannel(user, '  @mychannel  ');

    await waitFor(() => expect(apiPost).toHaveBeenCalledTimes(1));
    expect(apiPost).toHaveBeenCalledWith('/integrations/telegram/connect', {
      channel_id: '@mychannel',
    });
    expect(toast.success).toHaveBeenCalledWith('Telegram-канал подключён');
    expect(onClose).toHaveBeenCalled();
  });

  it('shows the email-verification message (not the bot-admin error) on a 412 gate', async () => {
    apiPost.mockRejectedValueOnce({
      response: {
        status: 412,
        data: { code: 'email_verification_required', verifiedDeadline: '2026-05-24T18:29:47Z' },
      },
    });
    const user = userEvent.setup();
    renderModal();

    await submitChannel(user, '@mychannel');

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(VERIFY_REQUIRED));
    expect(toast.error).not.toHaveBeenCalledWith(ADMIN_FAIL);
  });

  it('surfaces the backend error message for non-verification errors', async () => {
    const serverMsg = 'Добавьте бота администратором с правом публикации, затем переподключите.';
    apiPost.mockRejectedValueOnce({ response: { status: 409, data: { error: serverMsg } } });
    const user = userEvent.setup();
    renderModal();

    await submitChannel(user, '@mychannel');

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(serverMsg));
    expect(toast.error).not.toHaveBeenCalledWith(ADMIN_FAIL);
  });

  it('falls back to the generic failure when the error carries no message', async () => {
    apiPost.mockRejectedValueOnce(new Error('network down'));
    const user = userEvent.setup();
    renderModal();

    await submitChannel(user, '@mychannel');

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(ADMIN_FAIL));
  });
});
