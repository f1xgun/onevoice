import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mutable store proxy so we can flip activeBusinessId between renders.
let storeActiveBusinessId: string | null = null;
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: storeActiveBusinessId }),
}));

import { PermissionsCacheGuard } from '@/components/PermissionsCacheGuard';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';

function makeWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

beforeEach(() => {
  storeActiveBusinessId = null;
});

describe('PermissionsCacheGuard', () => {
  it('invalidates ["businesses", bizId, "permissions"] when activeBusinessId is set on mount', () => {
    storeActiveBusinessId = 'biz-1';
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const spy = vi.spyOn(qc, 'invalidateQueries');
    const Wrapper = makeWrapper(qc);
    render(
      <Wrapper>
        <PermissionsCacheGuard />
      </Wrapper>
    );
    expect(spy).toHaveBeenCalledWith({ queryKey: QUERY_KEYS.PERMISSIONS('biz-1') });
  });

  it('re-invalidates when activeBusinessId changes', () => {
    storeActiveBusinessId = 'biz-1';
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const spy = vi.spyOn(qc, 'invalidateQueries');
    const Wrapper = makeWrapper(qc);
    const { rerender } = render(
      <Wrapper>
        <PermissionsCacheGuard />
      </Wrapper>
    );
    expect(spy).toHaveBeenCalledTimes(1);
    expect(spy).toHaveBeenLastCalledWith({ queryKey: QUERY_KEYS.PERMISSIONS('biz-1') });

    storeActiveBusinessId = 'biz-2';
    rerender(
      <Wrapper>
        <PermissionsCacheGuard />
      </Wrapper>
    );
    expect(spy).toHaveBeenCalledTimes(2);
    expect(spy).toHaveBeenLastCalledWith({ queryKey: QUERY_KEYS.PERMISSIONS('biz-2') });
  });

  it('does NOT invalidate when activeBusinessId is null', () => {
    storeActiveBusinessId = null;
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const spy = vi.spyOn(qc, 'invalidateQueries');
    const Wrapper = makeWrapper(qc);
    render(
      <Wrapper>
        <PermissionsCacheGuard />
      </Wrapper>
    );
    expect(spy).not.toHaveBeenCalled();
  });

  it('renders no DOM', () => {
    storeActiveBusinessId = 'biz-1';
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const Wrapper = makeWrapper(qc);
    const { container } = render(
      <Wrapper>
        <PermissionsCacheGuard />
      </Wrapper>
    );
    expect(container.innerHTML).toBe('');
  });
});
