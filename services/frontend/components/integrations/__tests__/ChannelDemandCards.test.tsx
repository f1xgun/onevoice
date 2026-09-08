import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ChannelDemandCards } from '@/components/integrations/ChannelDemandCards';
import { bizApi } from '@/lib/api/business-api';

vi.mock('@/lib/api/business-api', () => ({ bizApi: vi.fn() }));
vi.mock('@/lib/telemetry', () => ({ trackEvent: vi.fn() }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

let allowed = true;
let permissionError = false;
const retryPermission = vi.fn();
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed, isError: permissionError, refetch: retryPermission }),
}));

const get = vi.fn();
const post = vi.fn();
const platforms = [
  { id: 'avito', fullLabel: 'Avito' },
  { id: '2gis', fullLabel: '2GIS' },
  { id: 'google_business', fullLabel: 'Google Business' },
  { id: 'whatsapp', fullLabel: 'WhatsApp' },
];

function showCards() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ChannelDemandCards businessId="org-a" platforms={platforms} />
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  allowed = true;
  permissionError = false;
  get.mockResolvedValue({ data: { channels: [] } });
  post.mockResolvedValue({});
  vi.mocked(bizApi).mockReturnValue({ get, post } as unknown as ReturnType<typeof bizApi>);
});
afterEach(cleanup);

describe('ChannelDemandCards', () => {
  it('offers only supported demand IDs and confirms the stored request', async () => {
    const user = userEvent.setup();
    showCards();
    const request = screen.getByRole('button', { name: 'Запросить канал Avito' });
    await waitFor(() => expect(request).toBeEnabled());
    expect(screen.getByRole('button', { name: 'Запросить канал Wildberries' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Запросить канал Ozon' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Запросить канал 2GIS' })).toBeEnabled();
    expect(
      screen.queryByRole('button', { name: /Google Business|WhatsApp/ })
    ).not.toBeInTheDocument();
    await user.click(request);
    expect(await screen.findByRole('button', { name: 'Запрос на Avito учтён' })).toBeDisabled();
    expect(post).toHaveBeenCalledExactlyOnceWith('/channel-requests', { channel: 'avito' });
  });

  it('shows history-load errors with retry and never pretends a request was saved', async () => {
    get.mockRejectedValueOnce(new Error('offline'));
    const user = userEvent.setup();
    showCards();
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить ваши запросы');
    expect(screen.getByRole('button', { name: 'Запросить канал Avito' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Повторить' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Запросить канал Avito' })).toBeEnabled()
    );
    expect(post).not.toHaveBeenCalled();
  });

  it('offers a permission retry instead of presenting load failure as denied access', async () => {
    permissionError = true;
    allowed = false;
    const user = userEvent.setup();
    showCards();
    await user.click(screen.getByRole('button'));
    expect(retryPermission).toHaveBeenCalledOnce();
    expect(get).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
  });

  it('renders English confirmation without exposing translation keys', async () => {
    (globalThis as unknown as { __setTestLocale: (locale: string) => void }).__setTestLocale('en');
    get.mockResolvedValue({ data: { channels: [{ channel: 'ozon', count: 1 }] } });
    showCards();
    expect(await screen.findByRole('button', { name: 'Ozon requested' })).toBeDisabled();
    expect(screen.getByText('Requested')).toBeInTheDocument();
  });
});
