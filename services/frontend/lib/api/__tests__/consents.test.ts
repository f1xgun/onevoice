import { describe, it, expect, vi, beforeEach } from 'vitest';

const authFetchMock = vi.hoisted(() => vi.fn());

vi.mock('@/lib/api/authFetch', () => ({
  authFetch: authFetchMock,
  refreshAccessToken: vi.fn(),
}));

vi.mock('@/lib/auth', () => ({
  useAuthStore: { getState: () => ({ accessToken: 'tok' }) },
}));

import { withdrawPDN } from '../consents';

describe('consents api client — withdrawPDN surfaces the scheduled deletion date', () => {
  beforeEach(() => vi.clearAllMocks());

  it('returns the deletion date from the 200 success body', async () => {
    authFetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        code: 'account_pending_deletion',
        deletionDate: '2026-07-24T12:00:00Z',
        restoreUrl: '/settings/account',
      }),
    } as unknown as Response);

    await expect(withdrawPDN()).resolves.toEqual({ deletionDate: '2026-07-24T12:00:00Z' });
    expect(authFetchMock.mock.calls[0][0]).toBe('/api/v1/users/me/consents/pdn/withdraw');
  });

  it('throws with the deletion date when deletion is already pending (423)', async () => {
    authFetchMock.mockResolvedValue({
      ok: false,
      status: 423,
      json: async () => ({
        code: 'account_pending_deletion',
        deletionDate: '2026-07-24T12:00:00Z',
        restoreUrl: '/settings/account',
      }),
    } as unknown as Response);

    await expect(withdrawPDN()).rejects.toMatchObject({
      status: 423,
      deletionDate: '2026-07-24T12:00:00Z',
    });
  });
});
