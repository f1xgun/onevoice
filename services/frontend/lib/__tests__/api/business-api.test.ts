import { describe, it, expect, vi, beforeEach } from 'vitest';
import { bizApi } from '../../api/business-api';

// Mock the api module
vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
    interceptors: {
      request: { use: vi.fn() },
      response: { use: vi.fn() },
    },
  },
}));

describe('bizApi', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('throws Error when bizId is empty string', () => {
    expect(() => bizApi('')).toThrow('bizApi: activeBusinessId is required');
  });

  it('get prefixes path with /businesses/{bizId}', async () => {
    const { api } = await import('@/lib/api');
    (api.get as ReturnType<typeof vi.fn>).mockResolvedValue({ data: [] });

    await bizApi('abc-uuid').get('/integrations');

    expect(api.get).toHaveBeenCalledWith('/businesses/abc-uuid/integrations', undefined);
  });

  it('post prefixes path and passes data', async () => {
    const { api } = await import('@/lib/api');
    (api.post as ReturnType<typeof vi.fn>).mockResolvedValue({ data: {} });

    await bizApi('abc-uuid').post('/conversations', { title: 'x' });

    expect(api.post).toHaveBeenCalledWith(
      '/businesses/abc-uuid/conversations',
      { title: 'x' },
      undefined
    );
  });

  it('all five verbs (get, post, put, patch, delete) prefix the path', async () => {
    const { api } = await import('@/lib/api');
    (api.get as ReturnType<typeof vi.fn>).mockResolvedValue({ data: {} });
    (api.post as ReturnType<typeof vi.fn>).mockResolvedValue({ data: {} });
    (api.put as ReturnType<typeof vi.fn>).mockResolvedValue({ data: {} });
    (api.patch as ReturnType<typeof vi.fn>).mockResolvedValue({ data: {} });
    (api.delete as ReturnType<typeof vi.fn>).mockResolvedValue({ data: {} });

    const helper = bizApi('test-id');

    await helper.get('/path');
    await helper.post('/path', {});
    await helper.put('/path', {});
    await helper.patch('/path', {});
    await helper.delete('/path');

    expect(api.get).toHaveBeenCalledWith('/businesses/test-id/path', undefined);
    expect(api.post).toHaveBeenCalledWith('/businesses/test-id/path', {}, undefined);
    expect(api.put).toHaveBeenCalledWith('/businesses/test-id/path', {}, undefined);
    expect(api.patch).toHaveBeenCalledWith('/businesses/test-id/path', {}, undefined);
    expect(api.delete).toHaveBeenCalledWith('/businesses/test-id/path', undefined);
  });

  it('optional config arg passes through to axios', async () => {
    const { api } = await import('@/lib/api');
    (api.get as ReturnType<typeof vi.fn>).mockResolvedValue({ data: {} });

    const signal = new AbortController().signal;
    await bizApi('abc-uuid').get('/data', { signal, params: { page: 1 } });

    expect(api.get).toHaveBeenCalledWith('/businesses/abc-uuid/data', {
      signal,
      params: { page: 1 },
    });
  });
});
