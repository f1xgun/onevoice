import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';

import { YandexBusinessConnectModal } from '../YandexBusinessConnectModal';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const BUSINESS_ID = 'biz-1';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: BUSINESS_ID }),
}));

const apiGet = vi.fn();
const apiPost = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: (...args: unknown[]) => apiGet(...args),
    post: (...args: unknown[]) => apiPost(...args),
    put: vi.fn(),
    delete: vi.fn(),
  }),
}));

function Wrapper({ client, children }: { client: QueryClient; children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderModal(onClose = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <Wrapper client={client}>
      <YandexBusinessConnectModal open={true} onClose={onClose} />
    </Wrapper>
  );
  return { ...utils, onClose };
}

function configAvailable() {
  apiGet.mockResolvedValue({ data: { available: true, rep_login: 'onevoice-rep' } });
}
function configUnavailable() {
  apiGet.mockResolvedValue({ data: { available: false, rep_login: '' } });
}

beforeEach(() => {
  apiGet.mockReset();
  apiPost.mockReset();
  vi.mocked(toast.success).mockReset();
  vi.mocked(toast.error).mockReset();
});

describe('YandexBusinessConnectModal — method routing', () => {
  it('leads with the delegated flow and shows the rep login when provisioned', async () => {
    configAvailable();
    renderModal();

    await waitFor(() =>
      expect(apiGet).toHaveBeenCalledWith('/integrations/yandex_business/delegated-config')
    );
    expect(await screen.findByText(/Дайте OneVoice доступ/)).toBeInTheDocument();
    expect(screen.getByText('onevoice-rep')).toBeInTheDocument();
    expect(screen.queryByText('Зачем нужны cookies?')).not.toBeInTheDocument();
  });

  it('shows a disabled delegated connection when unavailable', async () => {
    configUnavailable();
    renderModal();
    expect(await screen.findByText(/Подключение Яндекс.Бизнеса готовится/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Подключить$/ })).toBeDisabled();
    expect(screen.getByText(/Мы не запрашиваем пароли и cookies/)).toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(screen.queryByText(/Cookie-Editor|Подключить через cookies/)).not.toBeInTheDocument();
    expect(apiPost).not.toHaveBeenCalled();
  });

  it('never offers cookie or extension entry points when available', async () => {
    configAvailable();
    renderModal();
    await screen.findByText(/Дайте OneVoice доступ/);
    expect(screen.queryByText(/Cookie-Editor|Подключить через cookies/)).not.toBeInTheDocument();
    expect(screen.getByText(/Мы не запрашиваем пароли и cookies/)).toBeInTheDocument();
  });

  it('shows config verification errors inline without retrying', async () => {
    apiGet.mockRejectedValue({
      response: { status: 412, data: { code: 'email_verification_required' } },
    });
    renderModal();
    expect(await screen.findByRole('alert')).toHaveTextContent('Подтвердите email');
    expect(apiGet).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: /^Подключить$/ })).toBeDisabled();
  });
});

describe('YandexBusinessConnectModal — delegated connect', () => {
  it('connects with the pasted link, verifies access, and closes on success', async () => {
    configAvailable();
    apiPost
      .mockResolvedValueOnce({ data: { externalId: '114697172504' } })
      .mockResolvedValueOnce({ data: { access_verified: true } });
    const user = userEvent.setup();
    const { onClose } = renderModal();

    await screen.findByText(/Дайте OneVoice доступ/);
    await user.type(
      screen.getByLabelText('Ссылка на вашу организацию в Яндекс.Картах'),
      'https://yandex.ru/maps/org/kafe/114697172504/'
    );
    await user.click(screen.getByRole('button', { name: /^Подключить$/ }));

    await waitFor(() =>
      expect(apiPost).toHaveBeenCalledWith('/integrations/yandex_business/connect-delegated', {
        maps_url: 'https://yandex.ru/maps/org/kafe/114697172504/',
      })
    );
    await waitFor(() =>
      expect(apiPost).toHaveBeenCalledWith('/integrations/yandex_business/verify-access', {
        permalink: '114697172504',
      })
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(toast.success).toHaveBeenCalled();
  });

  it('shows the not-verified state without closing when access is not detected', async () => {
    configAvailable();
    apiPost
      .mockResolvedValueOnce({ data: { externalId: '114697172504' } })
      .mockResolvedValueOnce({ data: { access_verified: false } });
    const user = userEvent.setup();
    const { onClose } = renderModal();

    await screen.findByText(/Дайте OneVoice доступ/);
    await user.type(
      screen.getByLabelText('Ссылка на вашу организацию в Яндекс.Картах'),
      '114697172504'
    );
    await user.click(screen.getByRole('button', { name: /^Подключить$/ }));

    expect(await screen.findByText('Пока не видим доступ')).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});

it.each(['connect', 'verify', 'retry'])(
  'explains 412 at %s inline and stops repeat attempts',
  async (step) => {
    configAvailable();
    const gate = { response: { status: 412, data: { code: 'email_verification_required' } } };
    if (step !== 'connect') apiPost.mockResolvedValueOnce({ data: { externalId: '123' } });
    if (step === 'retry') apiPost.mockResolvedValueOnce({ data: { access_verified: false } });
    apiPost.mockRejectedValue(gate);
    const user = userEvent.setup();
    const { onClose } = renderModal();
    await user.type(
      await screen.findByLabelText('Ссылка на вашу организацию в Яндекс.Картах'),
      '123'
    );
    await user.click(screen.getByRole('button', { name: /^Подключить$/ }));
    if (step === 'retry')
      await user.click(await screen.findByRole('button', { name: 'Проверить снова' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Подтвердите email');
    expect(
      screen.getByRole('button', { name: step === 'retry' ? 'Проверить снова' : /^Подключить$/ })
    ).toBeDisabled();
    expect(onClose).not.toHaveBeenCalled();
    if (step !== 'retry')
      expect(screen.getByLabelText('Ссылка на вашу организацию в Яндекс.Картах')).toHaveValue(
        '123'
      );
  }
);
