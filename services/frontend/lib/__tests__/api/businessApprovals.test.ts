import { describe, it, expect, vi, beforeEach } from 'vitest';

const mockGet = vi.fn();
const mockPut = vi.fn();

vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: (path: string, config?: unknown) => mockGet(bizId, path, config),
    post: vi.fn(),
    put: (path: string, data?: unknown, config?: unknown) => mockPut(bizId, path, data, config),
    patch: vi.fn(),
    delete: vi.fn(),
  }),
}));

import {
  fetchBusinessToolApprovals,
  updateBusinessToolApprovals,
} from '@/lib/api/businessApprovals';

const BIZ_ID = 'test-biz-uuid';

describe('businessApprovals — bizApi migration', () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPut.mockReset();
  });

  it('fetchBusinessToolApprovals calls bizApi(bizId).get("/tool-approvals")', async () => {
    mockGet.mockResolvedValue({ data: { toolApprovals: { send_post: 'auto' } } });
    const result = await fetchBusinessToolApprovals(BIZ_ID);
    expect(mockGet).toHaveBeenCalledWith(BIZ_ID, '/tool-approvals', undefined);
    expect(result).toEqual({ send_post: 'auto' });
  });

  it('updateBusinessToolApprovals calls bizApi(bizId).put("/tool-approvals", { toolApprovals })', async () => {
    const approvals = { send_post: 'manual' as const };
    mockPut.mockResolvedValue({ data: { toolApprovals: approvals } });
    const result = await updateBusinessToolApprovals(BIZ_ID, approvals);
    expect(mockPut).toHaveBeenCalledWith(
      BIZ_ID,
      '/tool-approvals',
      { toolApprovals: approvals },
      undefined
    );
    expect(result).toEqual(approvals);
  });

  it('fetchBusinessToolApprovals does NOT use /business/{id}/tool-approvals path', async () => {
    mockGet.mockResolvedValue({ data: { toolApprovals: {} } });
    await fetchBusinessToolApprovals(BIZ_ID);
    const [[, path]] = mockGet.mock.calls;
    expect(path).not.toContain('/business/');
    expect(path).toBe('/tool-approvals');
  });
});
