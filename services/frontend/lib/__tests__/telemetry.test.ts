import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mutable token + a stub for the axios `api.post` used by the timer/size flush.
const h = vi.hoisted(() => ({
  token: 'tok-123' as string | null,
  businessId: null as string | null,
  postMock: vi.fn(),
}));

vi.mock('../api', () => ({ api: { post: h.postMock } }));
vi.mock('../auth', () => ({
  useAuthStore: { getState: () => ({ accessToken: h.token }) },
}));
vi.mock('../stores/business', () => ({
  useBusinessStore: { getState: () => ({ activeBusinessId: h.businessId }) },
}));

describe('telemetry — page-hide flush (AN-7)', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules(); // fresh module-level buffer per test
    vi.useFakeTimers();
    h.token = 'tok-123';
    h.businessId = null;
    h.postMock.mockReset().mockResolvedValue({ data: {} });
    fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('keeps approval correlation while discarding content, PII and forged server outcomes', async () => {
    const tele = await import('../telemetry');
    tele.trackEvent('approval', 'draft_shown', {
      page: '/reviews/person@example.com',
      correlationId: 'person@example.com',
      metadata: {
        draft_id: 'a'.repeat(64),
        kind: 'review_reply',
        source: 'reviews',
        text: 'PRIVATE',
        args: 'SECRET',
        author: 'person@example.com',
      },
    });
    tele.trackEvent('approval', 'send_result', { metadata: { draft_id: 'a'.repeat(64) } });
    await tele.flushTelemetry();
    const body = JSON.parse(fetchMock.mock.calls[0]![1].body);
    expect(body).toHaveLength(1);
    expect(body[0]).toMatchObject({
      page: '/reviews',
      correlationId: 'a'.repeat(64),
      metadata: { draft_id: 'a'.repeat(64), kind: 'review_reply', source: 'reviews' },
    });
    expect(JSON.stringify(body)).not.toMatch(/PRIVATE|SECRET|person@|send_result/);
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

  it('keeps the business captured when the event occurred', async () => {
    h.businessId = 'org-a';
    const tele = await import('../telemetry');
    tele.trackEvent('page_view', 'open');
    h.businessId = 'org-b';
    await tele.flushTelemetry();
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/businesses/org-a/telemetry');
  });

  it('uses a caller-captured business when a late mutation completes after an organization switch', async () => {
    h.businessId = 'org-a';
    const mutationBusinessId = h.businessId;
    const tele = await import('../telemetry');
    h.businessId = 'org-b';
    tele.trackEvent('activation', 'channel_demand_saved', { businessId: mutationBusinessId });
    await tele.flushTelemetry();
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/businesses/org-a/telemetry');
  });

  it('supports an explicit global scope even while a business is active', async () => {
    h.businessId = 'org-a';
    const tele = await import('../telemetry');
    tele.trackEvent('api_error', 'global', { businessId: null });
    await tele.flushTelemetry();
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/telemetry');
  });

  it('groups a mixed buffer by captured scope without sending trusted identifiers', async () => {
    const tele = await import('../telemetry');
    h.businessId = 'org-a';
    tele.trackEvent('page_view', 'a');
    h.businessId = 'org-b';
    tele.trackEvent('button_click', 'b');
    h.businessId = null;
    tele.trackEvent('api_error', 'global');
    await tele.flushTelemetry();
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      '/api/v1/businesses/org-a/telemetry',
      '/api/v1/businesses/org-b/telemetry',
      '/api/v1/telemetry',
    ]);
    for (const [, init] of fetchMock.mock.calls) {
      expect(init.keepalive).toBe(false);
      expect(init.body).not.toMatch(/businessId|userId/);
    }
  });

  it('always sends approval events globally', async () => {
    h.businessId = 'org-a';
    const tele = await import('../telemetry');
    tele.trackEvent('approval', 'draft_shown', {
      metadata: { draft_id: 'a'.repeat(64), kind: 'post', source: 'chat' },
      businessId: 'forged-business',
    });
    await tele.flushTelemetry();
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/telemetry');
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
