import { describe, expect, it } from 'vitest';

import { approvalKind } from '@/lib/approvalTelemetry';

describe('approvalKind', () => {
  it.each([
    ['telegram__send_channel_post', 'post'],
    ['telegram__send_channel_photo', 'post'],
    ['vk__publish_post', 'post'],
    ['vk__post_photo', 'post'],
    ['vk__schedule_post', 'post'],
    ['yandex_business__create_post', 'post'],
    ['telegram__reply_to_comment', 'review_reply'],
    ['vk__reply_comment', 'review_reply'],
    ['yandex_business__reply_review', 'review_reply'],
    ['google_business__reply_review', 'review_reply'],
  ] as const)('maps %s to %s', (toolName, kind) => {
    expect(approvalKind(toolName)).toBe(kind);
  });

  it('rejects non-publication tools and untrusted names', () => {
    expect(approvalKind('yandex_business__update_hours')).toBeUndefined();
    expect(approvalKind('private@example.com')).toBeUndefined();
  });
});
