import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, waitFor, act } from '@testing-library/react';
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

// Each business owns its own already-connected integrations. The page asks
// bizApi(id).get(path) for the integration list ('/integrations') and the
// business profile ('') — we route on path and resolve per active business.
const INTEGRATIONS_BY_BUSINESS: Record<string, { id: string; platform: string }[]> = {
  'biz-A': [{ id: 'int-A1', platform: 'telegram' }],
  'biz-B': [{ id: 'int-B1', platform: 'vk' }],
};

vi.mock('@/lib/api/business-api', () => ({
  bizApi: (id: string) => ({
    get: (path: string) => {
      if (path === '/integrations') {
        const rows = (INTEGRATIONS_BY_BUSINESS[id] ?? []).map((r) => ({
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

// Mutable active business id the store selector reads; switched mid-test to
// emulate the org switcher's setActive(b.id) without a remount.
let activeBusinessIdValue: string | null = 'biz-A';
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: activeBusinessIdValue }),
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

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('IntegrationsPage — whitelist banner on business switch', () => {
  let replaceStateSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    bannerRenders.mockReset();
    activeBusinessIdValue = 'biz-A';
    replaceStateSpy = vi.spyOn(window.history, 'replaceState').mockImplementation(() => {});
  });

  afterEach(() => {
    replaceStateSpy.mockRestore();
  });

  it('does not fire a fresh-registration banner when the active business switches A→B', async () => {
    const { rerender, queryByTestId } = render(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );

    // Baseline established with business A's already-connected integrations.
    await waitFor(() => expect(activeBusinessIdValue).toBe('biz-A'));
    expect(bannerRenders).not.toHaveBeenCalled();

    // Org switcher flips the active business without remounting the page.
    act(() => {
      activeBusinessIdValue = 'biz-B';
    });
    rerender(
      <Wrapper>
        <IntegrationsPage />
      </Wrapper>
    );

    // Give the integration list + profile queries for B time to resolve and
    // the detection effect to run against the new baseline.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });

    expect(bannerRenders).not.toHaveBeenCalled();
    expect(queryByTestId('whitelist-banner')).toBeNull();
  });
});
