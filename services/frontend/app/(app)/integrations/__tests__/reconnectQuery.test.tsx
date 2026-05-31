import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

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

// Capture which modal opens. Each modal mock spies on its `open` prop
// transitioning to true so the assertion below can verify the page
// flipped exactly one state setter.
const openSpies = {
  telegram: vi.fn(),
  vk: vi.fn(),
  yandex: vi.fn(),
  google: vi.fn(),
};

vi.mock('@/components/integrations/TelegramConnectModal', () => ({
  TelegramConnectModal: ({ open }: { open: boolean }) => {
    if (open) openSpies.telegram();
    return null;
  },
}));

vi.mock('@/components/integrations/VKCommunityModal', () => ({
  VKCommunityModal: ({ open }: { open: boolean }) => {
    if (open) openSpies.vk();
    return null;
  },
}));

vi.mock('@/components/integrations/YandexBusinessConnectModal', () => ({
  YandexBusinessConnectModal: ({ open }: { open: boolean }) => {
    if (open) openSpies.yandex();
    return null;
  },
}));

vi.mock('@/components/integrations/GoogleLocationModal', () => ({
  GoogleLocationModal: ({ open }: { open: boolean }) => {
    if (open) openSpies.google();
    return null;
  },
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

describe('IntegrationsPage — ?reconnect query handler', () => {
  let replaceStateSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    openSpies.telegram.mockReset();
    openSpies.vk.mockReset();
    openSpies.yandex.mockReset();
    openSpies.google.mockReset();
    searchParamsValue = {};
    replaceStateSpy = vi.spyOn(window.history, 'replaceState').mockImplementation(() => {});
  });

  afterEach(() => {
    replaceStateSpy.mockRestore();
  });

  it('reconnect=telegram opens the telegram modal exactly once', async () => {
    searchParamsValue = { reconnect: 'telegram' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await waitFor(() => expect(openSpies.telegram).toHaveBeenCalled());
    expect(openSpies.vk).not.toHaveBeenCalled();
    expect(openSpies.yandex).not.toHaveBeenCalled();
    expect(openSpies.google).not.toHaveBeenCalled();
  });

  it('reconnect=vk opens the vk community modal', async () => {
    searchParamsValue = { reconnect: 'vk' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await waitFor(() => expect(openSpies.vk).toHaveBeenCalled());
    expect(openSpies.telegram).not.toHaveBeenCalled();
  });

  it('reconnect=yandex_business opens the yandex modal', async () => {
    searchParamsValue = { reconnect: 'yandex_business' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await waitFor(() => expect(openSpies.yandex).toHaveBeenCalled());
  });

  it('reconnect=google_business opens the google location modal', async () => {
    searchParamsValue = { reconnect: 'google_business' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await waitFor(() => expect(openSpies.google).toHaveBeenCalled());
  });

  it('reconnect=unknown is silently ignored (no modal opens)', async () => {
    searchParamsValue = { reconnect: 'unknown_platform' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await new Promise((r) => setTimeout(r, 50));
    expect(openSpies.telegram).not.toHaveBeenCalled();
    expect(openSpies.vk).not.toHaveBeenCalled();
    expect(openSpies.yandex).not.toHaveBeenCalled();
    expect(openSpies.google).not.toHaveBeenCalled();
  });

  it('no reconnect param does not open any modal', async () => {
    searchParamsValue = {};
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await new Promise((r) => setTimeout(r, 50));
    expect(openSpies.telegram).not.toHaveBeenCalled();
    expect(openSpies.vk).not.toHaveBeenCalled();
    expect(openSpies.yandex).not.toHaveBeenCalled();
    expect(openSpies.google).not.toHaveBeenCalled();
  });

  it('reconnect param triggers replaceState to strip the query string', async () => {
    searchParamsValue = { reconnect: 'telegram' };
    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );
    await waitFor(() => expect(openSpies.telegram).toHaveBeenCalled());
    expect(replaceStateSpy).toHaveBeenCalledWith({}, '', '/integrations');
  });
});
