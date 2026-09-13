import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';

import { VKCommunityModal } from '../VKCommunityModal';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const BUSINESS_ID = 'biz-1';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: BUSINESS_ID }),
}));

const apiGet = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({ get: (...args: unknown[]) => apiGet(...args) }),
}));

function Wrapper({ client, children }: { client: QueryClient; children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderModal(onClose = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <Wrapper client={client}>
      <VKCommunityModal open onClose={onClose} />
    </Wrapper>
  );
  return { onClose };
}

describe('VKCommunityModal', () => {
  const originalLocation = window.location;

  beforeEach(() => {
    apiGet.mockReset();
    vi.mocked(toast.error).mockReset();
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: { ...originalLocation, href: '' },
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: originalLocation,
    });
  });

  it('opens the familiar VK sign-in flow', async () => {
    apiGet.mockResolvedValueOnce({
      data: { url: 'https://oauth.vk.com/authorize?client_id=1&state=abc' },
    });
    renderModal();

    await userEvent.setup().click(screen.getByRole('button', { name: 'Войти через ВКонтакте' }));

    await waitFor(() => expect(apiGet).toHaveBeenCalledWith('/integrations/vk/auth-url'));
    await waitFor(() =>
      expect(window.location.href).toBe('https://oauth.vk.com/authorize?client_id=1&state=abc')
    );
  });

  it('shows a plain connection error without credential or protocol instructions', async () => {
    apiGet.mockRejectedValueOnce(new Error('oauth_not_configured'));
    renderModal();

    await userEvent.setup().click(screen.getByRole('button', { name: 'Войти через ВКонтакте' }));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Не удалось подключить сообщество')
    );
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(screen.queryByText(/ключ доступа|токен|API/i)).not.toBeInTheDocument();
  });

  it('keeps cancellation reachable', async () => {
    const { onClose } = renderModal();
    await userEvent.setup().click(screen.getByRole('button', { name: 'Отмена' }));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
