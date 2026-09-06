import { describe, it, expect, vi, beforeEach } from 'vitest';

// bizApi is mocked so we can assert on the constructed path without a live backend.
const mockGet = vi.fn();
const mockPost = vi.fn();
const mockPut = vi.fn();
const mockDelete = vi.fn();

vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: (path: string, config?: unknown) => mockGet(bizId, path, config),
    post: (path: string, data?: unknown, config?: unknown) => mockPost(bizId, path, data, config),
    put: (path: string, data?: unknown, config?: unknown) => mockPut(bizId, path, data, config),
    delete: (path: string, config?: unknown) => mockDelete(bizId, path, config),
  }),
}));

import {
  listConversations,
  createConversation,
  moveConversation,
  pinConversation,
  unpinConversation,
  renameConversation,
  regenerateConversationTitle,
  deleteConversation,
} from '@/lib/conversations';

const BIZ_ID = 'test-biz-uuid';
const CONV_ID = 'conv-123';

describe('conversations — bizApi migration', () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    mockPut.mockReset();
    mockDelete.mockReset();
  });

  it('listConversations calls bizApi(bizId).get("/conversations")', async () => {
    mockGet.mockResolvedValue({ data: [] });
    await listConversations(BIZ_ID);
    expect(mockGet).toHaveBeenCalledWith(
      BIZ_ID,
      '/conversations',
      expect.objectContaining({ params: { limit: 100 } })
    );
  });

  it('listConversations preserves legacy successful null as an empty list', async () => {
    mockGet.mockResolvedValue({ data: null });
    const result = await listConversations(BIZ_ID);
    expect(result).toEqual([]);
  });

  it('createConversation calls bizApi(bizId).post("/conversations", input)', async () => {
    const input = { title: 'Test Chat', projectId: null };
    const conv = { id: CONV_ID, title: 'Test Chat' };
    mockPost.mockResolvedValue({ data: conv });
    const result = await createConversation(BIZ_ID, input);
    expect(mockPost).toHaveBeenCalledWith(BIZ_ID, '/conversations', input, undefined);
    expect(result).toEqual(conv);
  });

  it('moveConversation calls bizApi(bizId).post("/conversations/{id}/move")', async () => {
    const conv = { id: CONV_ID, title: 'Test' };
    mockPost.mockResolvedValue({ data: conv });
    await moveConversation(BIZ_ID, CONV_ID, 'proj-1');
    expect(mockPost).toHaveBeenCalledWith(
      BIZ_ID,
      `/conversations/${CONV_ID}/move`,
      { projectId: 'proj-1' },
      undefined
    );
  });

  it('pinConversation calls bizApi(bizId).post("/conversations/{id}/pin")', async () => {
    const conv = { id: CONV_ID, pinnedAt: '2026-01-01T00:00:00Z' };
    mockPost.mockResolvedValue({ data: conv });
    await pinConversation(BIZ_ID, CONV_ID);
    expect(mockPost).toHaveBeenCalledWith(
      BIZ_ID,
      `/conversations/${CONV_ID}/pin`,
      undefined,
      undefined
    );
  });

  it('unpinConversation calls bizApi(bizId).post("/conversations/{id}/unpin")', async () => {
    const conv = { id: CONV_ID, pinnedAt: null };
    mockPost.mockResolvedValue({ data: conv });
    await unpinConversation(BIZ_ID, CONV_ID);
    expect(mockPost).toHaveBeenCalledWith(
      BIZ_ID,
      `/conversations/${CONV_ID}/unpin`,
      undefined,
      undefined
    );
  });

  it('renameConversation calls bizApi(bizId).put("/conversations/{id}", { title })', async () => {
    const conv = { id: CONV_ID, title: 'New Title' };
    mockPut.mockResolvedValue({ data: conv });
    await renameConversation(BIZ_ID, CONV_ID, 'New Title');
    expect(mockPut).toHaveBeenCalledWith(
      BIZ_ID,
      `/conversations/${CONV_ID}`,
      { title: 'New Title' },
      undefined
    );
  });

  it('regenerateConversationTitle calls bizApi(bizId).post("/conversations/{id}/regenerate-title")', async () => {
    const conv = { id: CONV_ID, titleStatus: 'auto_pending' };
    mockPost.mockResolvedValue({ data: conv });
    await regenerateConversationTitle(BIZ_ID, CONV_ID);
    expect(mockPost).toHaveBeenCalledWith(
      BIZ_ID,
      `/conversations/${CONV_ID}/regenerate-title`,
      undefined,
      undefined
    );
  });

  it('deleteConversation calls bizApi(bizId).delete("/conversations/{id}")', async () => {
    mockDelete.mockResolvedValue({ data: undefined });
    await deleteConversation(BIZ_ID, CONV_ID);
    expect(mockDelete).toHaveBeenCalledWith(BIZ_ID, `/conversations/${CONV_ID}`, undefined);
  });
});

it.each([{}, { error: 'offline' }, 'invalid'])(
  'rejects malformed successful list payloads: %j',
  async (data) => {
    mockGet.mockResolvedValue({ data });
    await expect(listConversations(BIZ_ID)).rejects.toThrow('Invalid conversation list response');
  }
);
