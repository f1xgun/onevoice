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

  it('does not send and preserves the buffer when there is no access token', async () => {
    h.token = null;
    const tele = await import('../telemetry');
    tele.trackEvent('page_view', 'open');

    tele.flushOnHide();
    expect(fetchMock).not.toHaveBeenCalled();

    // A later authenticated flush still has the event — it was not dropped.
    h.token = 'tok-later';
    tele.flushOnHide();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body).toHaveLength(1);
  });

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
