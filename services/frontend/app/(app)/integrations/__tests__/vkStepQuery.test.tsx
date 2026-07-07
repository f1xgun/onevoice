import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';

// Mocks must be declared BEFORE importing the page module so the page's
// imports resolve to the mocked exports at module evaluation time.

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

vi.mock('@/lib/hooks/usePlatforms', () => ({
  usePlatforms: () => ({ platforms: [] }),
}));

vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: () => Promise.resolve({ data: [] }),
  }),
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: () => 'biz-test',
}));

vi.mock('@/lib/telemetry', () => ({
  trackClick: vi.fn(),
}));

const openSpies = {
  vkConnect: vi.fn(),
  vkPicker: vi.fn(),
};

vi.mock('@/components/integrations/TelegramConnectModal', () => ({
  TelegramConnectModal: () => null,
}));

vi.mock('@/components/integrations/VKCommunityModal', () => ({
  VKCommunityModal: ({ open }: { open: boolean }) => {
    if (open) openSpies.vkConnect();
    return null;
  },
}));

vi.mock('@/components/integrations/VKCommunityPickerModal', () => ({
  VKCommunityPickerModal: ({ open }: { open: boolean }) => {
    if (open) openSpies.vkPicker();
    return null;
  },
}));

vi.mock('@/components/integrations/YandexBusinessConnectModal', () => ({
  YandexBusinessConnectModal: () => null,
}));

vi.mock('@/components/integrations/GoogleLocationModal', () => ({
  GoogleLocationModal: () => null,
}));

vi.mock('@/components/integrations/PlatformCard', () => ({
  PlatformCard: () => null,
}));

vi.mock('@/components/integrations/WhitelistWarningBanner', () => ({
  WhitelistWarningBanner: () => null,
}));

// next/navigation: per-test useSearchParams stub.
let searchParamsValue: Record<string, string> = {};
vi.mock('next/navigation', () => ({
  useSearchParams: () => ({
    get: (key: string) => searchParamsValue[key] ?? null,
  }),
}));

import IntegrationsPage from '../page';

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('IntegrationsPage — ?vk_step query handler', () => {
  let replaceStateSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    openSpies.vkConnect.mockReset();
    openSpies.vkPicker.mockReset();
    vi.mocked(toast.success).mockReset();
    searchParamsValue = {};
    replaceStateSpy = vi.spyOn(window.history, 'replaceState').mockImplementation(() => {});
  });

  afterEach(() => {
    replaceStateSpy.mockRestore();
  });

  it('vk_step=select_community opens the picker once and strips the query string', async () => {
    searchParamsValue = { vk_step: 'select_community' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await waitFor(() => expect(openSpies.vkPicker).toHaveBeenCalled());
    expect(openSpies.vkPicker).toHaveBeenCalledTimes(1);
    expect(openSpies.vkConnect).not.toHaveBeenCalled();
    expect(replaceStateSpy).toHaveBeenCalledWith({}, '', '/integrations');
  });

  it('a different vk_step value opens nothing', async () => {
    searchParamsValue = { vk_step: 'something_else' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await new Promise((r) => setTimeout(r, 50));
    expect(openSpies.vkPicker).not.toHaveBeenCalled();
    expect(openSpies.vkConnect).not.toHaveBeenCalled();
  });

  it('no vk_step param opens nothing', async () => {
    searchParamsValue = {};
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await new Promise((r) => setTimeout(r, 50));
    expect(openSpies.vkPicker).not.toHaveBeenCalled();
  });

  it('connected=vk still fires the success toast and does not open the picker', async () => {
    searchParamsValue = { connected: 'vk' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith('Сообщество ВКонтакте подключено')
    );
    expect(openSpies.vkPicker).not.toHaveBeenCalled();
  });
});
