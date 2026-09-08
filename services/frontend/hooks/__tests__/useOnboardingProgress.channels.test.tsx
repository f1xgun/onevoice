import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useOnboardingProgress } from '../useOnboardingProgress';

const state = vi.hoisted(() => ({ businessId: 'org-a', get: vi.fn(), registryReady: true }));
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (select: (s: { activeBusinessId: string }) => unknown) =>
    select({ activeBusinessId: state.businessId }),
}));
vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => ({ data: [{ id: 'org-a' }], isSuccess: true }),
}));
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: true }) }));
vi.mock('@/lib/hooks/useMembers', () => ({ useMembers: () => ({ data: [] }) }));
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (id: string) => ({ get: (path: string) => state.get(id, path) }),
}));
vi.mock('@/lib/hooks/usePlatforms', () => ({
  usePlatforms: () => ({
    isSuccess: state.registryReady,
    platforms: [
      { id: 'telegram', fullLabel: 'Telegram', status: 'active' },
      { id: 'vk', fullLabel: 'VK', status: 'oauth_not_configured' },
      { id: 'yandex_business', fullLabel: 'Yandex', status: 'active' },
      { id: 'avito', fullLabel: 'Avito', status: 'coming_soon' },
      { id: 'google_business', fullLabel: 'Google', status: 'active' },
    ],
  }),
}));

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  return { client, ...renderHook(() => useOnboardingProgress(), { wrapper: Wrapper }) };
}

beforeEach(() => {
  state.businessId = 'org-a';
  state.registryReady = true;
  state.get.mockReset().mockImplementation(async (_id, path) => ({ data: path === '' ? {} : [] }));
});

describe('onboarding channel evidence', () => {
  it('shows only available platforms and waits for a successful registry response', async () => {
    state.registryReady = false;
    const { result, rerender } = setup();
    expect(result.current.channels).toEqual([]);
    state.registryReady = true;
    rerender();
    await waitFor(() => expect(result.current.channels?.[0].state).toBe('disconnected'));
    expect(result.current.channels?.map((channel) => channel.platform)).toEqual([
      'telegram',
      'yandex_business',
    ]);
    expect(result.current.channels?.[0].href).toBe('/integrations?connect=telegram');
  });

  it('flips connected after invalidation and keeps other channels optional', async () => {
    const { client, result } = setup();
    await waitFor(() => expect(result.current.channels?.[0].state).toBe('disconnected'));
    state.get.mockImplementation(async (_id, path) => ({
      data: path === '' ? {} : [{ platform: 'telegram', status: 'active' }],
    }));
    await act(async () => {
      await client.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS('org-a') });
    });
    await waitFor(() => expect(result.current.channels?.[0].state).toBe('connected'));
    expect(result.current.channels?.[0].canConnect).toBe(false);
    expect(result.current.channels?.[1].state).toBe('disconnected');
    expect(result.current.steps.find((step) => step.id === 'connectChannel')?.done).toBe(true);
    expect(result.current.total).toBe(4);
  });

  it('does not reuse another organization state while a new query is pending', async () => {
    state.get.mockImplementation((id, path) =>
      path === ''
        ? Promise.resolve({ data: {} })
        : id === 'org-a'
          ? Promise.resolve({ data: [{ platform: 'telegram', status: 'active' }] })
          : new Promise(() => {})
    );
    const { result, rerender } = setup();
    await waitFor(() => expect(result.current.channels?.[0].state).toBe('connected'));
    state.businessId = 'org-b';
    rerender();
    expect(result.current.channels?.[0].state).toBe('loading');
    expect(result.current.channels?.[0].canConnect).toBe(false);
    expect(result.current.steps.find((step) => step.id === 'connectChannel')?.done).toBe(false);
  });

  it.each([null, { unexpected: true }])('keeps malformed responses unknown: %j', async (data) => {
    state.get.mockResolvedValue({ data });
    const { result } = setup();
    await waitFor(() => expect(result.current.channels?.[0].state).toBe('unknown'));
    expect(result.current.channels?.[0].canConnect).toBe(false);
  });

  it('offers reconnect only on explicit evidence of a broken connection', async () => {
    state.get.mockImplementation(async (_id, path) => ({
      data: path === '' ? {} : [{ platform: 'telegram', status: 'token_expired' }],
    }));
    const { result } = setup();
    await waitFor(() => expect(result.current.channels?.[0].state).toBe('error'));
    expect(result.current.channels?.[0].href).toBe('/integrations?reconnect=telegram');
    expect(result.current.channels?.[0].canConnect).toBe(true);
  });
});
