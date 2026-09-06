import { describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
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
  it.each([
    [[], false],
    [[{ status: 'error' }], false],
    [[{ status: 'running' }], false],
    [[{ status: 'done' }], true],
  ])('derives completion from successful tasks: %j', async (tasks, done) => {
    get.mockImplementation(async (path: string) => ({ data: path === '/tasks' ? tasks : [] }));
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.loading).toBe(false)
    );
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(done);
    expect(get).not.toHaveBeenCalledWith('/conversations', expect.anything());
  });

  it('does not mark a failed task request complete', async () => {
    get.mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useOnboardingProgress(), { wrapper: Wrapper });
    await waitFor(() =>
      expect(result.current.steps.find((s) => s.id === 'firstAction')?.loading).toBe(false)
    );
    expect(result.current.steps.find((s) => s.id === 'firstAction')?.done).toBe(false);
  });
});
