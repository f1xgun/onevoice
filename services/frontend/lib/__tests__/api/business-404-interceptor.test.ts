import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from '@/lib/api';
import { useBusinessStore } from '@/lib/stores/business';
import { queryClient } from '@/lib/queryClient';
import { toast } from 'sonner';

vi.mock('sonner', () => ({ toast: { warning: vi.fn() } }));

describe('business availability on resource errors', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.clearAllMocks();
    useBusinessStore.getState().setActive('a');
  });

  async function reject(url: string, status = 404) {
    const error = { config: { url }, response: { status } };
    const interceptors = api.interceptors.response as unknown as {
      handlers: { rejected: (error: unknown) => Promise<unknown> }[];
    };
    await expect(interceptors.handlers[1].rejected(error)).rejects.toBe(error);
  }

  it('preserves membership when a nested conversation is missing', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: [{ id: 'a' }] });
    await reject('/businesses/a/conversations/missing');
    expect(useBusinessStore.getState().activeBusinessId).toBe('a');
    expect(toast.warning).not.toHaveBeenCalled();
  });

  it('clears only a confirmed inaccessible active organization', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: [{ id: 'b' }] });
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
    await reject('/businesses/a/members');
    expect(useBusinessStore.getState().activeBusinessId).toBeNull();
    expect(queryClient.getQueryData(['businesses'])).toEqual([{ id: 'b' }]);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['businesses'], exact: true });
    await vi.waitFor(() => expect(toast.warning).toHaveBeenCalledOnce());
  });

  it('preserves the organization if membership verification fails', async () => {
    vi.spyOn(api, 'get').mockRejectedValue(new Error('offline'));
    await reject('/businesses/a/conversations/missing');
    expect(useBusinessStore.getState().activeBusinessId).toBe('a');
  });

  it('ignores a late response from the previous organization', async () => {
    const get = vi.spyOn(api, 'get');
    useBusinessStore.getState().setActive('b');
    await reject('/businesses/a/conversations/missing');
    expect(get).not.toHaveBeenCalled();
    expect(useBusinessStore.getState().activeBusinessId).toBe('b');
  });

  it('rechecks the active organization after awaiting memberships', async () => {
    vi.spyOn(api, 'get').mockImplementation(async () => {
      useBusinessStore.getState().setActive('b');
      return { data: [] };
    });
    await reject('/businesses/a/conversations/missing');
    expect(useBusinessStore.getState().activeBusinessId).toBe('b');
  });

  it.each([
    ['/auth/me', 404],
    ['/businesses/a/members', 500],
  ])('ignores unrelated error %s %s', async (url, status) => {
    const get = vi.spyOn(api, 'get');
    await reject(url, status);
    expect(get).not.toHaveBeenCalled();
    expect(useBusinessStore.getState().activeBusinessId).toBe('a');
  });
});
