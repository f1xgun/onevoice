import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';

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

// One business with a mutable set of already-connected integrations. The page
// asks bizApi(id).get(path) for the integration list ('/integrations') and the
// business profile ('') — we route on path and read the live array so a connect
// that lands between the baseline render and a warm refetch is observable.
const integrationsForBusiness: { id: string; platform: string }[] = [
  { id: 'int-B1', platform: 'vk' },
];

vi.mock('@/lib/api/business-api', () => ({
  bizApi: (id: string) => ({
    get: (path: string) => {
      if (path === '/integrations') {
        const rows = integrationsForBusiness.map((r) => ({
          ...r,
          status: 'active',
          externalId: r.id,
          createdAt: '2026-01-01T00:00:00Z',
        }));
        return Promise.resolve({ data: rows });
      }
      return Promise.resolve({ data: { id, name: id } });
    },
  }),
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-B' }),
}));

vi.mock('@/lib/telemetry', () => ({
  trackClick: vi.fn(),
}));

vi.mock('@/components/integrations/TelegramConnectModal', () => ({
  TelegramConnectModal: () => null,
}));

vi.mock('@/components/integrations/VKCommunityModal', () => ({
  VKCommunityModal: () => null,
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

// Records every fresh-registration prompt the page mounts.
const bannerRenders = vi.fn();
vi.mock('@/components/integrations/WhitelistWarningBanner', () => ({
  WhitelistWarningBanner: (props: { integrationId: string; businessId: string }) => {
    bannerRenders(props);
    return <div data-testid="whitelist-banner" />;
  },
}));

vi.mock('next/navigation', () => ({
  useSearchParams: () => ({ get: () => null }),
}));

import IntegrationsPage from '../page';

describe('IntegrationsPage — whitelist banner on same-business connect', () => {
  let replaceStateSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    bannerRenders.mockReset();
    integrationsForBusiness.length = 0;
    integrationsForBusiness.push({ id: 'int-B1', platform: 'vk' });
    replaceStateSpy = vi.spyOn(window.history, 'replaceState').mockImplementation(() => {});
  });

  afterEach(() => {
    replaceStateSpy.mockRestore();
  });

  it('fires the fresh-registration banner when a new integration arrives via a warm-cache refetch on the same business', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    }

    render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );

    // Baseline established from the one already-connected integration; no banner
    // for rows present at first load.
    await waitFor(() =>
      expect(client.getQueryData(QUERY_KEYS.BUSINESS_INTEGRATIONS('biz-B'))).toHaveLength(1)
    );
    expect(bannerRenders).not.toHaveBeenCalled();

    // A connect lands: the modal's onClose invalidates the integration list,
    // which on a warm cache is a background refetch (isLoading stays false)
    // that grows the list from 1 to 2.
    integrationsForBusiness.push({ id: 'int-B2', platform: 'telegram' });
    await act(async () => {
      await client.invalidateQueries({
        queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS('biz-B'),
      });
    });

    await waitFor(() => expect(bannerRenders).toHaveBeenCalled());
    const lastCall = bannerRenders.mock.calls.at(-1)?.[0] as {
      integrationId: string;
      businessId: string;
    };
    expect(lastCall.integrationId).toBe('int-B2');
    expect(lastCall.businessId).toBe('biz-B');
  });
});
