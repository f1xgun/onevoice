import { describe, it, expect, vi, beforeEach } from 'vitest';

const mockGet = vi.fn();

vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: (path: string, config?: unknown) => mockGet(bizId, path, config),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  }),
}));

import { fetchTools } from '@/lib/api/tools';

const BIZ_ID = 'test-biz-uuid';

describe('tools — bizApi migration', () => {
  beforeEach(() => {
    mockGet.mockReset();
  });

  it('fetchTools(bizId) calls bizApi(bizId).get("/tools")', async () => {
    const toolList = [
      {
        name: 'telegram__send_post',
        platform: 'telegram',
        floor: 'manual',
        editableFields: [],
        description: 'd',
      },
    ];
    mockGet.mockResolvedValue({ data: toolList });
    const result = await fetchTools(BIZ_ID);
    expect(mockGet).toHaveBeenCalledWith(BIZ_ID, '/tools', undefined);
    expect(result[0]?.name).toBe('telegram__send_post');
    expect(result[0]?.platform).toBe('telegram');
  });

  it('fetchTools does NOT call bare /tools (auth-only path gone)', async () => {
    mockGet.mockResolvedValue({ data: [] });
    await fetchTools(BIZ_ID);
    // The path passed to get must be '/tools', not '/api/v1/tools'
    const [[, path]] = mockGet.mock.calls;
    expect(path).toBe('/tools');
  });

  it('fetchTools validates response schema and returns typed Tool[]', async () => {
    mockGet.mockResolvedValue({
      data: [
        {
          name: 'vk__publish_post',
          platform: 'vk',
          floor: 'auto',
          editableFields: [],
          description: 'Publish a post',
        },
      ],
    });
    const tools = await fetchTools(BIZ_ID);
    expect(tools[0]?.name).toBe('vk__publish_post');
    expect(tools[0]?.platform).toBe('vk');
  });
});
