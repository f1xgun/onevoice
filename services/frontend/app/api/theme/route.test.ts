import type { POST } from './route';
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
const setCookie = vi.hoisted(() => vi.fn());
vi.mock('next/headers', () => ({ cookies: async () => ({ set: setCookie }) }));

let post: typeof POST;
function request(body: unknown, ip = 'test') {
  return new Request('http://localhost/api/theme', {
    method: 'POST',
    headers: { 'x-forwarded-for': ip },
    body: JSON.stringify(body),
  });
}
beforeEach(async () => {
  vi.resetModules();
  setCookie.mockReset();
  post = (await import('./route')).POST;
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

describe('theme route', () => {
  it.each(['system', 'light', 'dark'])('persists %s for a year', async (theme) => {
    vi.stubEnv('NODE_ENV', 'production');
    const response = await post(request({ theme }));
    expect(response.status).toBe(204);
    expect(await response.text()).toBe('');
    expect(setCookie).toHaveBeenCalledWith({
      name: 'NEXT_THEME',
      value: theme,
      httpOnly: false,
      sameSite: 'lax',
      path: '/',
      maxAge: 31536000,
      secure: true,
    });
  });
  it('allows the cookie over HTTP outside production', async () => {
    vi.stubEnv('NODE_ENV', 'development');
    await post(request({ theme: 'system' }));
    expect(setCookie).toHaveBeenCalledWith(expect.objectContaining({ secure: false }));
  });
  it.each([null, {}, [], { theme: 'auto' }, { theme: 1 }, 'dark'])('rejects %j', async (body) => {
    expect((await post(request(body))).status).toBe(400);
    expect(setCookie).not.toHaveBeenCalled();
  });
  it('rejects malformed JSON', async () => {
    expect(
      (await post(new Request('http://localhost/api/theme', { method: 'POST', body: '{' }))).status
    ).toBe(400);
    expect(setCookie).not.toHaveBeenCalled();
  });
  it('limits each IP to 30 requests and refills after a minute', async () => {
    vi.useFakeTimers();
    for (let i = 0; i < 30; i++)
      expect((await post(request({ theme: 'dark' }, 'first, proxy'))).status).toBe(204);
    expect((await post(request({ theme: 'light' }, 'first, other'))).status).toBe(429);
    expect(setCookie).toHaveBeenCalledTimes(30);
    expect((await post(request({ theme: 'light' }, 'second'))).status).toBe(204);
    vi.advanceTimersByTime(60000);
    expect((await post(request({ theme: 'light' }, 'first'))).status).toBe(204);
  });
});
