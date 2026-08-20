// app/(app)/posts/_components/BroadcastBadge.tsx — status pill for a merged
// broadcast row. Unlike the per-post StatusBadge it speaks in channels:
// "published to N channels" when every channel landed, "M of N — VK: error"
// when some failed, so a partial failure is visible without expanding the row.
'use client';

import { useTranslations } from 'next-intl';

import { Badge } from '@/components/ui/badge';

import { isChannelError, PLATFORM_SHORT_KEYS, type BroadcastChannelResult } from '../_helpers';

export function BroadcastBadge({ channels }: { channels: BroadcastChannelResult[] }) {
  const tPosts = useTranslations('posts');
  const tShort = useTranslations('posts.platformShort');

  const total = channels.length;
  const failed = channels.filter((c) => isChannelError(c.result));

  if (failed.length === 0) {
    return (
      <Badge tone="success" dot>
        {tPosts('broadcast.publishedAll', { count: total })}
      </Badge>
    );
  }
  if (failed.length === total) {
    return (
      <Badge tone="danger" dot>
        {tPosts('broadcast.failedAll', { count: total })}
      </Badge>
    );
  }
  const failedNames = [
    ...new Set(
      failed.map((c) => (PLATFORM_SHORT_KEYS.has(c.platform) ? tShort(c.platform) : c.platform))
    ),
  ].join(', ');
  return (
    <Badge tone="danger" dot>
      {tPosts('broadcast.partial', {
        ok: total - failed.length,
        total,
        channels: failedNames,
      })}
    </Badge>
  );
}
