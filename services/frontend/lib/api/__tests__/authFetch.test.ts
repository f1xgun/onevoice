import { describe, it, expect, vi, beforeEach } from 'vitest';

// Hoisted spies so the vi.mock factories (which are hoisted above imports)
// can reference them.
const h = vi.hoisted(() => ({
  setAuth: vi.fn(),
  setAccessToken: vi.fn(),
  logout: vi.fn(),
  state: { token: 'token-1' as string | null },
}));

const axiosH = vi.hoisted(() => ({ post: vi.fn() }));

vi.mock('axios', () => ({ default: { post: axiosH.post } }));

vi.mock('@/lib/auth', () => ({
  useAuthStore: {
    getState: () => ({
      accessToken: h.state.token,
      setAuth: h.setAuth,
      setAccessToken: h.setAccessToken,
      logout: h.logout,
    }),
  },
}));

import { authFetch } from '../authFetch';

function resp(status: number, body?: unknown): Response {
  return {
    status,
    ok: status < 400,
    json: async () => body ?? {},
  } as unknown as Response;
}

function authHeader(init: RequestInit | undefined): string | null {
  return new Headers(init?.headers).get('Authorization');
}

describe('authFetch', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    h.state.token = 'token-1';
    // Park on /login so the refresh-failure redirect guard is a no-op and
    // jsdom never logs "Not implemented: navigation".
    vi.stubGlobal('location', { pathname: '/login', href: '' });
  });

  it('attaches the bearer token and passes a non-401 response straight through', async () => {
    const fetchMock = vi.fn().mockResolvedValue(resp(200, { ok: true }));
    vi.stubGlobal('fetch', fetchMock);

    const res = await authFetch('/api/v1/x', { method: 'GET' });

    expect(res.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/x');
    expect(authHeader(fetchMock.mock.calls[0][1])).toBe('Bearer token-1');
    expect(axiosH.post).not.toHaveBeenCalled();
  });

  it('omits the Authorization header when there is no token', async () => {
    h.state.token = null;
    const fetchMock = vi.fn().mockResolvedValue(resp(200));
    vi.stubGlobal('fetch', fetchMock);

    await authFetch('/api/v1/x');

    expect(authHeader(fetchMock.mock.calls[0][1])).toBeNull();
  });

  it('refreshes once on 401 and replays the request with the new token', async () => {
    h.state.token = 'old';
    const fetchMock = vi.fn().mockResolvedValueOnce(resp(401)).mockResolvedValueOnce(resp(204));
    vi.stubGlobal('fetch', fetchMock);
    axiosH.post.mockResolvedValue({ data: { accessToken: 'new' } });

    const res = await authFetch('/api/v1/x', { method: 'POST', body: '{}' });

    expect(res.status).toBe(204);
    expect(axiosH.post).toHaveBeenCalledTimes(1);
    expect(h.setAccessToken).toHaveBeenCalledWith('new');
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(authHeader(fetchMock.mock.calls[1][1])).toBe('Bearer new');
  });

  it('hydrates the full user when the refresh response carries one', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(resp(401)).mockResolvedValueOnce(resp(200));
    vi.stubGlobal('fetch', fetchMock);
    const user = { id: 'u1', email: 'a@b.c', name: 'A' };
    axiosH.post.mockResolvedValue({ data: { accessToken: 'new', user } });

    await authFetch('/api/v1/x');

    expect(h.setAuth).toHaveBeenCalledWith(user, 'new');
    expect(h.setAccessToken).not.toHaveBeenCalled();
  });

  it('logs out and returns the original 401 when the refresh itself 401s (dead cookie)', async () => {
    const fetchMock = vi.fn().mockResolvedValue(resp(401, { code: 'unauthorized' }));
    vi.stubGlobal('fetch', fetchMock);
    // axios surfaces the /auth/refresh 401 as an error with `response.status`.
    axiosH.post.mockRejectedValue({ response: { status: 401 } });

    const res = await authFetch('/api/v1/x');

    expect(res.status).toBe(401);
    expect(h.logout).toHaveBeenCalledTimes(1);
    // No retry — the request was attempted exactly once.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it.each([
    { name: 'rate limit', error: { response: { status: 429 } } },
    { name: 'server failure', error: { response: { status: 500 } } },
    { name: 'network failure', error: new Error('Network Error') },
  ])('does not log out on refresh $name', async ({ error }) => {
    const fetchMock = vi.fn().mockResolvedValue(resp(401, { code: 'unauthorized' }));
    vi.stubGlobal('fetch', fetchMock);
    axiosH.post.mockRejectedValue(error);

    const res = await authFetch('/api/v1/x');

    expect(res.status).toBe(401);
    expect(h.logout).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('shares a single in-flight refresh across concurrent 401s', async () => {
    h.state.token = 'old';
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(resp(401))
      .mockResolvedValueOnce(resp(401))
      .mockResolvedValue(resp(204));
    vi.stubGlobal('fetch', fetchMock);

    let resolveRefresh!: (v: unknown) => void;
    axiosH.post.mockReturnValue(
      new Promise((r) => {
        resolveRefresh = r;
      })
    );

    const pA = authFetch('/a');
    const pB = authFetch('/b');

    // Let both requests hit their 401 and arrive at the shared refresh await.
    await new Promise((r) => setTimeout(r, 0));
    expect(axiosH.post).toHaveBeenCalledTimes(1);

    resolveRefresh({ data: { accessToken: 'new' } });
    const [rA, rB] = await Promise.all([pA, pB]);

    expect(rA.status).toBe(204);
    expect(rB.status).toBe(204);
    expect(axiosH.post).toHaveBeenCalledTimes(1);
  });
});
