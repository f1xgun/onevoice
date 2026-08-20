// app/(app)/posts/_components/PlatformResultCard.tsx — per-platform
// result row inside the ExpandedPanel right column. When the platform
// reported a public permalink, the row carries an "open" external link so
// each channel of a broadcast can be opened individually.
import { useTranslations } from 'next-intl';
import { ExternalLink } from 'lucide-react';

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
      {ok && result.url && (
        <a
          href={result.url}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={`${tPosts('openLink')} — ${display}`}
          title={tPosts('openLink')}
          className="inline-flex shrink-0 items-center text-ink-soft transition-colors hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ExternalLink aria-hidden className="size-3.5" />
        </a>
      )}
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
