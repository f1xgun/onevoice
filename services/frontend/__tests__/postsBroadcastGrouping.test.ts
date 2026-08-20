import { describe, it, expect } from 'vitest';

import { mergeBroadcastGroups } from '@/app/(app)/posts/_helpers';
import type { Post } from '@/types/post';

function post(overrides: Partial<Post> & { id: string }): Post {
  return {
    businessId: 'biz-1',
    content: 'hello',
    status: 'published',
    createdAt: '2026-08-01T10:00:00Z',
    ...overrides,
  };
}

describe('mergeBroadcastGroups', () => {
  it('keeps legacy posts without a group id as standalone rows', () => {
    const rows = mergeBroadcastGroups([post({ id: 'a' }), post({ id: 'b' })]);
    expect(rows).toHaveLength(2);
    expect(rows.map((r) => r.id)).toEqual(['a', 'b']);
    expect(rows[0].broadcastChannels).toBeUndefined();
  });

  it('keeps a single-member group as a plain row', () => {
    const rows = mergeBroadcastGroups([post({ id: 'a', broadcastGroupId: 'g1' })]);
    expect(rows).toHaveLength(1);
    expect(rows[0].id).toBe('a');
    expect(rows[0].broadcastChannels).toBeUndefined();
  });

  it('collapses a broadcast group into one row with per-channel results', () => {
    const rows = mergeBroadcastGroups([
      post({
        id: 'a',
        broadcastGroupId: 'g1',
        platformResults: {
          telegram: { postId: '1', url: 'https://t.me/c/1', status: 'published' },
        },
      }),
      post({
        id: 'b',
        broadcastGroupId: 'g1',
        platformResults: {
          vk: { postId: '2', url: 'https://vk.com/wall-1_2', status: 'published' },
        },
      }),
      post({ id: 'z' }),
    ]);
    expect(rows).toHaveLength(2);
    const merged = rows[0];
    expect(merged.id).toBe('broadcast-g1');
    expect(merged.status).toBe('published');
    expect(merged.broadcastChannels).toHaveLength(2);
    expect(Object.keys(merged.platformResults ?? {}).sort()).toEqual(['telegram', 'vk']);
    expect(rows[1].id).toBe('z');
  });

  it('derives partial status and keeps the failed channel visible', () => {
    const rows = mergeBroadcastGroups([
      post({
        id: 'a',
        broadcastGroupId: 'g1',
        platformResults: { telegram: { postId: '1', url: '', status: 'published' } },
      }),
      post({
        id: 'b',
        broadcastGroupId: 'g1',
        status: 'error',
        platformResults: { vk: { postId: '', url: '', status: 'error', error: 'rate limited' } },
      }),
    ]);
    expect(rows).toHaveLength(1);
    expect(rows[0].status).toBe('partial');
    const failed = rows[0].broadcastChannels?.filter((c) => c.result.error);
    expect(failed?.map((c) => c.platform)).toEqual(['vk']);
  });

  it('derives error status when every channel failed', () => {
    const rows = mergeBroadcastGroups([
      post({
        id: 'a',
        broadcastGroupId: 'g1',
        status: 'error',
        platformResults: { telegram: { postId: '', url: '', status: 'error', error: 'boom' } },
      }),
      post({
        id: 'b',
        broadcastGroupId: 'g1',
        status: 'error',
        platformResults: { vk: { postId: '', url: '', status: 'error', error: 'boom' } },
      }),
    ]);
    expect(rows).toHaveLength(1);
    expect(rows[0].status).toBe('error');
  });

  it('never merges posts of different groups', () => {
    const rows = mergeBroadcastGroups([
      post({ id: 'a', broadcastGroupId: 'g1' }),
      post({ id: 'b', broadcastGroupId: 'g2' }),
      post({ id: 'c', broadcastGroupId: 'g1' }),
      post({ id: 'd', broadcastGroupId: 'g2' }),
    ]);
    expect(rows.map((r) => r.id).sort()).toEqual(['broadcast-g1', 'broadcast-g2']);
  });

  it('prefers the errored duplicate for the platform chip map so a failure is not masked', () => {
    const rows = mergeBroadcastGroups([
      post({
        id: 'a',
        broadcastGroupId: 'g1',
        platformResults: { vk: { postId: '1', url: '', status: 'published' } },
      }),
      post({
        id: 'b',
        broadcastGroupId: 'g1',
        status: 'error',
        platformResults: { vk: { postId: '', url: '', status: 'error', error: 'boom' } },
      }),
    ]);
    expect(rows).toHaveLength(1);
    expect(rows[0].platformResults?.vk.error).toBe('boom');
    expect(rows[0].broadcastChannels).toHaveLength(2);
  });
});
