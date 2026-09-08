import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useChannelDemand } from '@/hooks/useChannelDemand';
import { bizApi } from '@/lib/api/business-api';
import { trackEvent } from '@/lib/telemetry';

vi.mock('@/lib/api/business-api', () => ({ bizApi: vi.fn() }));
vi.mock('@/lib/telemetry', () => ({ trackEvent: vi.fn() }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const get = vi.fn();
const post = vi.fn();

function setup(businessId = 'org-a') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  return renderHook(({ id, canRead, canWrite }) => useChannelDemand(id, canRead, canWrite), {
    wrapper,
    initialProps: { id: businessId, canRead: true, canWrite: true },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  get.mockResolvedValue({ data: { channels: [] } });
  post.mockResolvedValue({});
  vi.mocked(bizApi).mockReturnValue({ get, post } as unknown as ReturnType<typeof bizApi>);
});

afterEach(cleanup);

describe('useChannelDemand', () => {
  it('persists one request on a double click, then confirms and tracks only the saved request', async () => {
    let finish!: () => void;
    post.mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      })
    );
    const { result } = setup();
    await waitFor(() => expect(result.current.isReady).toBe(true));

    act(() => {
      result.current.request('avito');
      result.current.request('avito');
    });
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    expect(post).toHaveBeenCalledWith('/channel-requests', { channel: 'avito' });
    expect(trackEvent).not.toHaveBeenCalled();
    expect(result.current.requested.has('avito')).toBe(false);

    await act(async () => finish());
    await waitFor(() => expect(result.current.requested.has('avito')).toBe(true));
    expect(trackEvent).toHaveBeenCalledExactlyOnceWith('activation', 'waitlist_platform', {
      page: '/integrations',
      metadata: { platform: 'avito', business_id: 'org-a' },
      businessId: 'org-a',
    });
    act(() => result.current.request('avito'));
    expect(post).toHaveBeenCalledTimes(1);
  });

  it('restores confirmation from persisted counts and keeps it scoped to the organization', async () => {
    get.mockResolvedValueOnce({ data: { channels: [{ channel: '2gis', count: 1 }] } });
    const { result, rerender } = setup();
    await waitFor(() => expect(result.current.requested.has('2gis')).toBe(true));
    act(() => result.current.request('2gis'));
    expect(post).not.toHaveBeenCalled();
    rerender({ id: 'org-b', canRead: true, canWrite: true });
    await waitFor(() => expect(result.current.isReady).toBe(true));
    expect(result.current.requested.has('2gis')).toBe(false);
    expect(bizApi).toHaveBeenLastCalledWith('org-b');
    expect(trackEvent).not.toHaveBeenCalled();
  });

  it('keeps failed writes retryable without emitting success telemetry', async () => {
    post.mockRejectedValueOnce(new Error('network'));
    const { result } = setup();
    await waitFor(() => expect(result.current.isReady).toBe(true));
    act(() => result.current.request('ozon'));
    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current.requested.has('ozon')).toBe(false);
    expect(trackEvent).not.toHaveBeenCalled();
    act(() => result.current.request('ozon'));
    await waitFor(() => expect(result.current.requested.has('ozon')).toBe(true));
    expect(post).toHaveBeenCalledTimes(2);
  });

  it('keeps a late save in the originating organization after switching', async () => {
    let finish!: () => void;
    post.mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      })
    );
    const { result, rerender } = setup();
    await waitFor(() => expect(result.current.isReady).toBe(true));
    act(() => result.current.request('wildberries'));
    await waitFor(() => expect(post).toHaveBeenCalledOnce());
    rerender({ id: 'org-b', canRead: true, canWrite: true });
    await waitFor(() => expect(result.current.isReady).toBe(true));
    await act(async () => finish());
    expect(result.current.requested.has('wildberries')).toBe(false);
    expect(trackEvent).toHaveBeenCalledWith('activation', 'waitlist_platform', {
      page: '/integrations',
      metadata: { platform: 'wildberries', business_id: 'org-a' },
      businessId: 'org-a',
    });
    rerender({ id: 'org-a', canRead: true, canWrite: true });
    expect(result.current.requested.has('wildberries')).toBe(true);
  });

  it('never writes before history loads, after a history error, or without permission', async () => {
    get.mockRejectedValueOnce(new Error('offline'));
    const { result, rerender } = setup();
    act(() => result.current.request('avito'));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.request('avito'));
    expect(post).not.toHaveBeenCalled();
    await act(async () => {
      await result.current.refetch();
    });
    await waitFor(() => expect(result.current.isReady).toBe(true));
    rerender({ id: 'org-a', canRead: true, canWrite: false });
    act(() => result.current.request('avito'));
    expect(post).not.toHaveBeenCalled();
  });
});
