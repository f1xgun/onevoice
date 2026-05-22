// app/(app)/posts/_components/PlatformResultCard.tsx — per-platform
// result row inside the ExpandedPanel right column.
//
// Extracted from posts/page.tsx as part of Phase 19 / 19-12.
import { useTranslations } from 'next-intl';
import { ChannelMark } from '@/components/ui/channel-mark';
import { CHANNEL_NAMES } from '@/lib/platforms';
import type { Post } from '@/types/post';

import { PLATFORM_SHORT_KEYS } from '../_helpers';

export function PlatformResultCard({
  platform,
  result,
}: {
  platform: string;
  result: NonNullable<Post['platformResults']>[string];
}) {
  const tPosts = useTranslations('posts');
  const tShort = useTranslations('posts.platformShort');
  const shortLabel = PLATFORM_SHORT_KEYS.has(platform) ? tShort(platform) : platform;
  const ok = !result.error && (result.status === 'published' || result.status === 'ok');
  const display = CHANNEL_NAMES[platform as keyof typeof CHANNEL_NAMES] ?? shortLabel;
  return (
    <div className="flex items-center gap-2.5 rounded-sm border border-line-soft bg-paper px-3 py-2">
      <ChannelMark name={display} size={20} />
      <span className="flex-1 truncate text-[13px] text-ink-mid">
        {ok
          ? result.url
            ? tPosts('stats.publishedLabel')
            : shortLabel
          : (result.error ?? result.status)}
      </span>
      <span
        aria-hidden
        className={
          'size-1.5 shrink-0 rounded-full ' +
          (ok ? 'bg-[var(--ov-success)]' : 'bg-[var(--ov-ink-faint)]')
        }
      />
    </div>
  );
}
