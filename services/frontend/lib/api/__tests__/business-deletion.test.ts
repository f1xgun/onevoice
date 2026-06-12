import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { deleteBusiness, restoreBusiness, type BusinessDeletionError } from '../business-deletion';

vi.mock('@/lib/auth', () => ({
  useAuthStore: {
    getState: () => ({ accessToken: 'test-token' }),
  },
}));

function mockFetch(status: number, body?: unknown) {
  const fn = vi.fn().mockResolvedValue({
    status,
    json: async () => body ?? {},
  } as Response);
  vi.stubGlobal('fetch', fn);
  return fn;
}

describe('business-deletion api client', () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.unstubAllGlobals());

  describe('deleteBusiness', () => {
    it('resolves void on 204', async () => {
      const fn = mockFetch(204);
      await expect(deleteBusiness('biz-1')).resolves.toBeUndefined();
      expect(fn).toHaveBeenCalledWith(
        '/api/v1/businesses/biz-1',
        expect.objectContaining({ method: 'DELETE' })
      );
    });

    it('throws with code + status on 403 not_organization_owner', async () => {
      mockFetch(403, { code: 'not_organization_owner' });
      await expect(deleteBusiness('biz-1')).rejects.toMatchObject({
        code: 'not_organization_owner',
        status: 403,
      });
    });

    it('throws with the 423 pending-deletion payload', async () => {
      mockFetch(423, {
        code: 'business_pending_deletion',
        deletionDate: '2026-07-01T00:00:00Z',
        restoreUrl: '/business',
      });
      await expect(deleteBusiness('biz-1')).rejects.toMatchObject({
        code: 'business_pending_deletion',
        status: 423,
        restoreUrl: '/business',
      });
    });

    it('attaches the status even when the body is unparsable', async () => {
      const fn = vi.fn().mockResolvedValue({
        status: 500,
        json: async () => {
          throw new Error('not json');
        },
      } as unknown as Response);
      vi.stubGlobal('fetch', fn);
      try {
        await deleteBusiness('biz-1');
        throw new Error('expected throw');
      } catch (e) {
        expect((e as BusinessDeletionError).status).toBe(500);
      }
    });
  });

  describe('restoreBusiness', () => {
    it('resolves void on 204', async () => {
      const fn = mockFetch(204);
      await expect(restoreBusiness('biz-1')).resolves.toBeUndefined();
      expect(fn).toHaveBeenCalledWith(
        '/api/v1/businesses/biz-1/restore',
        expect.objectContaining({ method: 'POST' })
      );
    });

    it('throws deletion_too_old on 410', async () => {
      mockFetch(410, { code: 'deletion_too_old' });
      await expect(restoreBusiness('biz-1')).rejects.toMatchObject({
        code: 'deletion_too_old',
        status: 410,
      });
    });
  });
});
