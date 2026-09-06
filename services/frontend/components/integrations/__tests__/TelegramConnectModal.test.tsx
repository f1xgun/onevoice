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

  it.each([
    [412, { code: 'email_verification_required' }, VERIFY_REQUIRED],
    [400, { reason: 'api_rejected', error: 'localized guidance' }, 'Канал не найден.'],
    [409, { reason: 'not_admin' }, 'Добавьте бота администратором'],
    [409, { reason: 'no_post_rights' }, 'Добавьте бота администратором'],
    [409, { reason: 'already_connected' }, 'Этот канал уже подключён к другой организации.'],
    [403, { reason: 'forbidden' }, 'Нет доступа к каналу.'],
    [429, { reason: 'rate_limited' }, 'Telegram временно ограничил запросы.'],
    [502, { reason: 'unreachable' }, 'Не удалось связаться с Telegram.'],
    [
      409,
      { error: 'Этот канал уже подключён к другой организации' },
      'Этот канал уже подключён к другой организации. Отключите его от неё или выберите другой канал.',
    ],
    [409, { reason: 'unknown' }, 'Не удалось подключить канал.'],
    [409, { reason: 'toString' }, 'Не удалось подключить канал.'],
    [400, { error: 'Bad Request: chat not found' }, 'Не удалось подключить канал.'],
    [undefined, undefined, 'Не удалось связаться с Telegram.'],
  ])(
    'shows a localized inline reason for %s and preserves input and focus',
    async (status, data, text) => {
      apiPost.mockRejectedValueOnce(status ? { response: { status, data } } : new Error('network'));
      const user = userEvent.setup();
      const { onClose } = renderModal();
      await submitChannel(user, '@mychannel');
      expect(await screen.findByRole('alert')).toHaveTextContent(text as string);
      expect(screen.getByPlaceholderText(/@channel/)).toHaveValue('@mychannel');
      expect(screen.getByPlaceholderText(/@channel/)).toHaveFocus();
      expect(onClose).not.toHaveBeenCalled();
      expect(toast.error).not.toHaveBeenCalled();
    }
  );
});
