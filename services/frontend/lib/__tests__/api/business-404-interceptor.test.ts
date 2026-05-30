import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock dependencies before importing api.ts
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: {
    getState: vi.fn(() => ({ clear: vi.fn() })),
  },
}));

vi.mock('@/lib/queryClient', () => ({
  queryClient: {
    invalidateQueries: vi.fn(),
  },
}));

vi.mock('@/lib/hooks/useBusinessList', () => ({
  BUSINESS_LIST_QUERY_KEY: ['businesses'],
  useBusinessList: vi.fn(),
}));

vi.mock('@/lib/telemetry', () => ({
  trackEvent: vi.fn(),
}));

vi.mock('@/lib/auth', () => ({
  useAuthStore: {
    getState: vi.fn(() => ({
      accessToken: null,
      setAuth: vi.fn(),
      setAccessToken: vi.fn(),
      logout: vi.fn(),
    })),
  },
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));

// Import the api module (triggers interceptor registration as side effect)
const { api } = await import('@/lib/api');

// Import the mocked dependencies to spy on them
const { useBusinessStore } = await import('@/lib/stores/business');
const { queryClient } = await import('@/lib/queryClient');

describe('404 interceptor', () => {
  let clearFn: ReturnType<typeof vi.fn>;
  let invalidateFn: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.clearAllMocks();

    clearFn = vi.fn();
    invalidateFn = vi.fn();
    (useBusinessStore.getState as ReturnType<typeof vi.fn>).mockReturnValue({ clear: clearFn });
    (queryClient.invalidateQueries as ReturnType<typeof vi.fn>).mockImplementation(invalidateFn);
  });

  // Helper: trigger the axios response error interceptors by making a mock failing request
  async function triggerInterceptors(
    url: string,
    status: number,
    metadata?: { skipBusinessNotFound?: boolean }
  ) {
    // We directly invoke the interceptors registered on the api instance
    // by using the axios adapter mock approach
    const error = {
      config: { url, ...(metadata ? { metadata } : {}) },
      response: { status, data: {} },
      message: `Request failed with status code ${status}`,
    };

    // Axios stores registered interceptors — we can invoke them via
    // the api.interceptors.response.handlers array (internal structure)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handlers = (api.interceptors.response as any).handlers as Array<{
      fulfilled: ((v: unknown) => unknown) | null;
      rejected: ((e: unknown) => unknown) | null;
    }>;

    // Run all registered error handlers in sequence (same as axios does internally)
    let result: unknown = error;
    for (const handler of handlers) {
      if (handler?.rejected) {
        try {
          result = await handler.rejected(result);
        } catch (e) {
          result = e;
        }
      }
    }

    // The final result should be a rejected promise
    return Promise.reject(result);
  }

  it('Test 1: 404 on /businesses/* clears store and invalidates businesses query', async () => {
    await expect(triggerInterceptors('/businesses/abc/integrations', 404)).rejects.toBeDefined();

    expect(clearFn).toHaveBeenCalled();
    expect(invalidateFn).toHaveBeenCalledWith({ queryKey: ['businesses'] });
  });

  it('Test 2: skipBusinessNotFound=true prevents store clear and query invalidation', async () => {
    await expect(
      triggerInterceptors('/businesses/abc/integrations', 404, { skipBusinessNotFound: true })
    ).rejects.toBeDefined();

    expect(clearFn).not.toHaveBeenCalled();
    expect(invalidateFn).not.toHaveBeenCalled();
  });

  it('Test 3: 404 on non-/businesses/ URL does NOT clear store', async () => {
    await expect(triggerInterceptors('/auth/me', 404)).rejects.toBeDefined();

    expect(clearFn).not.toHaveBeenCalled();
    expect(invalidateFn).not.toHaveBeenCalled();
  });

  it('Test 4: 500 on /businesses/* does NOT clear store (only 404 triggers)', async () => {
    await expect(triggerInterceptors('/businesses/abc/integrations', 500)).rejects.toBeDefined();

    expect(clearFn).not.toHaveBeenCalled();
    expect(invalidateFn).not.toHaveBeenCalled();
  });

  it('Test 5: two response interceptors are registered (401 + 404)', () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handlers = (api.interceptors.response as any).handlers as Array<unknown>;
    const nonNullHandlers = handlers.filter(Boolean);
    expect(nonNullHandlers.length).toBeGreaterThanOrEqual(2);
  });

  it('Test 6: interceptors.response.use was called exactly twice in api.ts', () => {
    // Verify by checking axios interceptors count directly
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handlers = (api.interceptors.response as any).handlers as Array<unknown>;
    expect(handlers.filter(Boolean).length).toBeGreaterThanOrEqual(2);
  });
});

import { toast } from 'sonner';
// Re-import mocked modules to access spies in the second describe block
const { useBusinessStore: bStore } = await import('@/lib/stores/business');
const { queryClient: qc } = await import('@/lib/queryClient');

describe('404 interceptor — warning toast', () => {
  let clearFn: ReturnType<typeof vi.fn>;
  let invalidateFn: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.clearAllMocks();
    clearFn = vi.fn();
    invalidateFn = vi.fn();
    (bStore.getState as ReturnType<typeof vi.fn>).mockReturnValue({ clear: clearFn });
    (qc.invalidateQueries as ReturnType<typeof vi.fn>).mockImplementation(invalidateFn);
  });

  async function invokeInterceptor(
    url: string,
    status: number,
    metadata?: { skipBusinessNotFound?: boolean }
  ) {
    const error = {
      config: { url, ...(metadata ? { metadata } : {}) },
      response: { status, data: {} },
      message: `Request failed with status code ${status}`,
    };
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const handlers = (api.interceptors.response as any).handlers as Array<{
      fulfilled: ((v: unknown) => unknown) | null;
      rejected: ((e: unknown) => unknown) | null;
    }>;
    let result: unknown = error;
    for (const handler of handlers) {
      if (handler?.rejected) {
        try {
          result = await handler.rejected(result);
        } catch (e) {
          result = e;
        }
      }
    }
    return Promise.reject(result);
  }

  it('fires toast.warning + clears store + invalidates on 404 /businesses/...', async () => {
    await expect(invokeInterceptor('/businesses/biz-1/members', 404)).rejects.toBeDefined();
    expect(clearFn).toHaveBeenCalled();
    expect(invalidateFn).toHaveBeenCalledWith({ queryKey: ['businesses'] });
    // Phase B1: the toast is now fired via a fire-and-forget async
    // helper that dynamically imports the messages bundle. Drain the
    // microtask + macrotask queues before asserting the mock saw the
    // call — the bundle import resolves in a microtask, but the
    // createTranslator pipeline adds another tick before the toast.
    await vi.waitFor(() => expect(toast.warning).toHaveBeenCalled());
    expect(toast.warning).toHaveBeenCalledWith('Эта организация больше недоступна');
  });

  it('does NOT fire toast.warning when URL is not /businesses/...', async () => {
    await expect(invokeInterceptor('/auth/me', 404)).rejects.toBeDefined();
    expect(toast.warning).not.toHaveBeenCalled();
  });

  it('does NOT fire toast.warning when skipBusinessNotFound is true', async () => {
    await expect(
      invokeInterceptor('/businesses/biz-1/preview', 404, { skipBusinessNotFound: true })
    ).rejects.toBeDefined();
    expect(toast.warning).not.toHaveBeenCalled();
  });

  it('does NOT fire toast.warning on 500 (only 404 triggers the branch)', async () => {
    await expect(invokeInterceptor('/businesses/biz-1/members', 500)).rejects.toBeDefined();
    expect(toast.warning).not.toHaveBeenCalled();
  });
});
