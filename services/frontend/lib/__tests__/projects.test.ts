import { describe, it, expect, vi, beforeEach } from 'vitest';

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
  listProjects,
  getProject,
  createProject,
  updateProject,
  deleteProject,
  getConversationCount,
} from '@/lib/projects';

const BIZ_ID = 'test-biz-uuid';
const PROJ_ID = 'proj-123';

describe('projects — bizApi migration', () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    mockPut.mockReset();
    mockDelete.mockReset();
  });

  it('listProjects calls bizApi(bizId).get("/projects")', async () => {
    mockGet.mockResolvedValue({ data: [] });
    await listProjects(BIZ_ID);
    expect(mockGet).toHaveBeenCalledWith(BIZ_ID, '/projects', undefined);
  });

  it('listProjects returns array even when data is not array', async () => {
    mockGet.mockResolvedValue({ data: null });
    const result = await listProjects(BIZ_ID);
    expect(result).toEqual([]);
  });

  it('getProject calls bizApi(bizId).get("/projects/{id}")', async () => {
    const proj = { id: PROJ_ID, name: 'My Project' };
    mockGet.mockResolvedValue({ data: proj });
    const result = await getProject(BIZ_ID, PROJ_ID);
    expect(mockGet).toHaveBeenCalledWith(BIZ_ID, `/projects/${PROJ_ID}`, undefined);
    expect(result).toEqual(proj);
  });

  it('createProject calls bizApi(bizId).post("/projects", input)', async () => {
    const input = { name: 'New Project' };
    const proj = { id: PROJ_ID, name: 'New Project' };
    mockPost.mockResolvedValue({ data: proj });
    const result = await createProject(BIZ_ID, input);
    expect(mockPost).toHaveBeenCalledWith(BIZ_ID, '/projects', input, undefined);
    expect(result).toEqual(proj);
  });

  it('updateProject calls bizApi(bizId).put("/projects/{id}", input)', async () => {
    const input = { name: 'Updated' };
    const proj = { id: PROJ_ID, name: 'Updated' };
    mockPut.mockResolvedValue({ data: proj });
    const result = await updateProject(BIZ_ID, PROJ_ID, input);
    expect(mockPut).toHaveBeenCalledWith(BIZ_ID, `/projects/${PROJ_ID}`, input, undefined);
    expect(result).toEqual(proj);
  });

  it('deleteProject calls bizApi(bizId).delete("/projects/{id}")', async () => {
    const resp = { deletedConversations: 2, deletedMessages: 10 };
    mockDelete.mockResolvedValue({ data: resp });
    const result = await deleteProject(BIZ_ID, PROJ_ID);
    expect(mockDelete).toHaveBeenCalledWith(BIZ_ID, `/projects/${PROJ_ID}`, undefined);
    expect(result).toEqual(resp);
  });

  it('getConversationCount calls bizApi(bizId).get("/projects/{id}/conversation-count")', async () => {
    mockGet.mockResolvedValue({ data: { count: 5 } });
    const result = await getConversationCount(BIZ_ID, PROJ_ID);
    expect(mockGet).toHaveBeenCalledWith(
      BIZ_ID,
      `/projects/${PROJ_ID}/conversation-count`,
      undefined
    );
    expect(result).toBe(5);
  });
});
