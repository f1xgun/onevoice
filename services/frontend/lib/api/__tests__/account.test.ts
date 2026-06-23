import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Pins the deliberate carve-out in account.ts: deleteAccount must stay on
// raw fetch (NOT authFetch), because its 401 means `password_invalid`, not
// session-expiry — routing it through authFetch would fire a token refresh
// on every wrong password. restoreAccount, whose 401 IS session-expiry,
// goes through authFetch. A future "consistency" refactor that routes
// deleteAccount through authFetch should fail this test.

const authFetchMock = vi.hoisted(() => vi.fn());

vi.mock('@/lib/api/authFetch', () => ({
  authFetch: authFetchMock,
  refreshAccessToken: vi.fn(),
}));

vi.mock('@/lib/auth', () => ({
  useAuthStore: { getState: () => ({ accessToken: 'tok' }) },
}));

import { deleteAccount, restoreAccount } from '../account';

describe('account api client — deleteAccount stays raw', () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.unstubAllGlobals());

  it('deleteAccount uses raw fetch (never authFetch) and does NOT refresh on a 401', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 401,
      json: async () => ({ code: 'password_invalid' }),
    } as unknown as Response);
    vi.stubGlobal('fetch', fetchMock);

    await expect(deleteAccount('wrong-password')).rejects.toMatchObject({
      code: 'password_invalid',
      status: 401,
    });

    // The carve-out: authFetch (and thus the shared refresh) is never touched.
    expect(authFetchMock).not.toHaveBeenCalled();
    // Raw fetch with the inline bearer header.
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/users/me');
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe('DELETE');
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer tok');
  });

  it('restoreAccount goes through authFetch (its 401 is session-expiry)', async () => {
    authFetchMock.mockResolvedValue({ status: 204, json: async () => ({}) } as unknown as Response);

    await expect(restoreAccount()).resolves.toBeUndefined();

    expect(authFetchMock).toHaveBeenCalledTimes(1);
    expect(authFetchMock.mock.calls[0][0]).toBe('/api/v1/users/me/restore');
  });
});
