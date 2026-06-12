import { describe, expect, it } from 'vitest';

import { titlePollInterval } from '../useConversations';
import type { Conversation, TitleStatus } from '@/lib/conversations';

const NOW = 1_700_000_000_000;

function conv(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: 'c1',
    userId: 'u1',
    businessId: 'b1',
    projectId: null,
    title: 'Новый чат',
    titleStatus: 'auto_pending' as TitleStatus,
    createdAt: new Date(NOW).toISOString(),
    updatedAt: new Date(NOW).toISOString(),
    ...overrides,
  };
}

describe('titlePollInterval — auto_pending poll give-up bound', () => {
  it('returns false when data is undefined', () => {
    expect(titlePollInterval(undefined, NOW)).toBe(false);
  });

  it('returns false for an empty list', () => {
    expect(titlePollInterval([], NOW)).toBe(false);
  });

  it('polls (2000ms) when a fresh chat is auto_pending', () => {
    expect(titlePollInterval([conv()], NOW)).toBe(2000);
  });

  it('gives up (false) once the auto_pending chat is older than the bound', () => {
    const stale = conv({ createdAt: new Date(NOW - 61_000).toISOString() });
    expect(titlePollInterval([stale], NOW)).toBe(false);
  });

  it('does not poll for resolved (auto) titles', () => {
    expect(titlePollInterval([conv({ titleStatus: 'auto' })], NOW)).toBe(false);
  });

  it('polls when at least one fresh chat is pending among resolved ones', () => {
    const data = [conv({ id: 'old', titleStatus: 'manual' }), conv({ id: 'fresh' })];
    expect(titlePollInterval(data, NOW)).toBe(2000);
  });
});
