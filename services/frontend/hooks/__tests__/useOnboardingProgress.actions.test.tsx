import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useOnboardingProgress } from '../useOnboardingProgress';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (select: (s: { activeBusinessId: string }) => unknown) =>
    select({ activeBusinessId: 'org' }),
}));
vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: () => ({ data: [{ id: 'org' }], isSuccess: true }),
}));
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: false }) }));
vi.mock('@/lib/hooks/useMembers', () => ({ useMembers: () => ({ data: [] }) }));
const { get } = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock('@/lib/api/business-api', () => ({ bizApi: () => ({ get }) }));

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('first action completion', () => {
  beforeEach(() => {
    get.mockReset();
  });

  it.each([
    ['old success with twenty newer empty chats', true],
    ['no success', false],
    ['failed turn with status error', false],
    ['successful task', true],
    ['legacy response without an authoritative flag', undefined],
  ])('uses the organization flag: %s', async (_name, flag) => {
    get.mockImplementation(async (path: string) => ({
      data:
        path === ''
          ? { hasFirstSuccessfulAction: flag }
          : path === '/conversations'
            ? Array.from({ length: 20 }, (_, i) => ({ id: String(i) }))
            : [],
    }));
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(flag === true);
    expect(get.mock.calls.map(([path]) => path).sort()).toEqual(['', '/integrations']);
  });

  it('refreshes completion after the profile is invalidated', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    let answered = false;
    get.mockImplementation(async (path: string) => ({
      data: path === '' ? { hasFirstSuccessfulAction: answered } : [],
    }));
    function Provider({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    }
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Provider });
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(false);
    answered = true;
    await act(async () => {
      await client.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE('org') });
    });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(true)
    );
  });

  it('treats a null profile as incomplete', async () => {
    get.mockResolvedValue({ data: null });
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(false);
  });

  it('does not mark an unavailable profile complete', async () => {
    get.mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(false);
  });
});
