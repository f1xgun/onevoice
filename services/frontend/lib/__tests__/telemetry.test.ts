import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mutable token + a stub for the axios `api.post` used by the timer/size flush.
const h = vi.hoisted(() => ({
  token: 'tok-123' as string | null,
  postMock: vi.fn(),
}));

vi.mock('../api', () => ({ api: { post: h.postMock } }));
vi.mock('../auth', () => ({
  useAuthStore: { getState: () => ({ accessToken: h.token }) },
}));

describe('telemetry — page-hide flush (AN-7)', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules(); // fresh module-level buffer per test
    vi.useFakeTimers();
    h.token = 'tok-123';
    h.postMock.mockReset().mockResolvedValue({ data: {} });
    fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('posts buffered events via keepalive fetch with the Authorization header', async () => {
    const tele = await import('../telemetry');
    tele.trackEvent('page_view', 'open', { page: '/dashboard' });

    tele.flushOnHide();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/v1/telemetry');
    expect(init.method).toBe('POST');
    expect(init.keepalive).toBe(true);
    expect(init.credentials).toBe('include'); // mirror the axios client's withCredentials
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer tok-123');
    const body = JSON.parse(init.body as string);
    expect(body).toHaveLength(1);
    expect(body[0]).toMatchObject({ eventType: 'page_view', action: 'open', page: '/dashboard' });
  });

  it('drops anonymous events including delayed flushes and later login', async () => {
    h.token = null;
    const tele = await import('../telemetry');
    tele.trackEvent('api_error', 'register conflict');
    await vi.advanceTimersByTimeAsync(5000);
    tele.flushOnHide();
    h.token = 'later';
    await tele.flushTelemetry();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(h.postMock).not.toHaveBeenCalled();
  });

  it('drops a pending batch when logged out before its timer fires', async () => {
    const tele = await import('../telemetry');
    tele.trackEvent('page_view', 'open');
    h.token = null;
    await vi.advanceTimersByTimeAsync(5000);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each([401, 429, 500])(
    'does not retry or use the auth client after telemetry HTTP %s',
    async (status) => {
      fetchMock.mockResolvedValue(new Response(null, { status }));
      const tele = await import('../telemetry');
      tele.trackEvent('api_error', 'failed request');
      await vi.advanceTimersByTimeAsync(15000);
      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(h.postMock).not.toHaveBeenCalled();
      expect(window.location.pathname).not.toBe('/login');
    }
  );

  it('no-ops when the buffer is empty', async () => {
    const tele = await import('../telemetry');
    tele.flushOnHide();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('clears the buffer after flushing so a second hide does not double-send', async () => {
    const tele = await import('../telemetry');
    tele.trackEvent('page_view', 'open');

    tele.flushOnHide();
    tele.flushOnHide();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
